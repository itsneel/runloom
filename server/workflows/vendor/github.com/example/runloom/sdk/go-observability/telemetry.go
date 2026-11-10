// Package observability provides bounded, payload-free operational telemetry.
package observability

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type Telemetry struct {
	gateway  bool
	metrics  *sdkmetric.MeterProvider
	logs     *sdklog.LoggerProvider
	logger   otellog.Logger
	requests metric.Int64Counter
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

// Start uses only explicit file-derived configuration, never OTEL environment
// auto-configuration. The OTLP listener is local to the pod/network namespace.
func Start(ctx context.Context, service, endpoint string) (*Telemetry, error) {
	if endpoint == "" {
		return &Telemetry{gateway: service == "gateway"}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:4318" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("telemetry endpoint must be http://127.0.0.1:4318")
	}
	switch service {
	case "auth", "workflows", "management", "gateway", "worker", "assistant":
	default:
		return nil, errors.New("unknown telemetry service")
	}
	res := resource.NewSchemaless(attribute.String("service.name", service))
	le, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint+"/v1/logs"), otlploghttp.WithHeaders(map[string]string{}), otlploghttp.WithTimeout(time.Second), otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, errors.New("initialize log exporter failed")
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(le, sdklog.WithMaxQueueSize(1024), sdklog.WithExportMaxBatchSize(128), sdklog.WithExportInterval(time.Second), sdklog.WithExportTimeout(time.Second))))
	me, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"), otlpmetrichttp.WithHeaders(map[string]string{}), otlpmetrichttp.WithTimeout(time.Second), otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}))
	if err != nil {
		_ = lp.Shutdown(ctx)
		return nil, errors.New("initialize metric exporter failed")
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(5*time.Second), sdkmetric.WithTimeout(time.Second))))
	t := &Telemetry{gateway: service == "gateway", metrics: mp, logs: lp, logger: lp.Logger("runloom")}
	meter := mp.Meter("runloom")
	t.requests, err = meter.Int64Counter("runloom.http.requests", metric.WithDescription("Completed HTTP requests"))
	if err != nil {
		return nil, err
	}
	t.duration, err = meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30))
	if err != nil {
		return nil, err
	}
	t.active, err = meter.Int64UpDownCounter("http.server.active_requests")
	if err != nil {
		return nil, err
	}
	t.event(ctx, "service.started")
	return t, nil
}

func (t *Telemetry) event(ctx context.Context, name string, attrs ...otellog.KeyValue) {
	if t.logger == nil {
		return
	}
	var r otellog.Record
	r.SetTimestamp(time.Now())
	r.SetSeverity(otellog.SeverityInfo)
	r.SetBody(otellog.StringValue(name))
	r.AddAttributes(attrs...)
	t.logger.Emit(ctx, r)
}

// Close has a fixed deadline even when the collector has already stopped.
func (t *Telemetry) Close() {
	if t.metrics == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	t.event(ctx, "service.stopped")
	_ = t.logs.Shutdown(ctx)
	_ = t.metrics.Shutdown(ctx)
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *response) WriteHeader(code int) {
	if code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *response) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *response) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func method(value string) string {
	switch value {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return value
	}
	return "OTHER"
}

func route(r *http.Request) string {
	// Pattern is populated by Go's ServeMux, not a request header or URL value.
	if r.Pattern != "" {
		return r.Pattern
	}
	for _, p := range []string{"/healthz", "/readyz", "/readyz/vault", "/api/v1/health"} {
		if r.URL.Path == p {
			return p
		}
	}
	// Gateway and pre-routing failures get bounded families, never raw IDs/queries.
	for _, p := range []string{"/api/v1/studio/", "/api/v1/", "/internal/v1/"} {
		if strings.HasPrefix(r.URL.Path, p) {
			return p + "*"
		}
	}
	return "unmatched"
}

func (t *Telemetry) Wrap(next http.Handler) http.Handler {
	if t.metrics == nil {
		return correlate(next, t.gateway)
	}
	return correlate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m := method(r.Method)
		active := metric.WithAttributes(attribute.String("http.request.method", m))
		t.active.Add(r.Context(), 1, active)
		id := RequestID(r.Context())
		out := &response{ResponseWriter: w}
		finished := false
		defer func() {
			status := out.status
			if !finished {
				status = 500
			}
			if status == 0 {
				status = 200
			}
			elapsed := time.Since(start).Seconds()
			path := route(r)
			attrs := metric.WithAttributes(attribute.String("http.request.method", m), attribute.String("http.route", path), attribute.Int("http.response.status_code", status))
			t.requests.Add(r.Context(), 1, attrs)
			t.duration.Record(r.Context(), elapsed, attrs)
			t.active.Add(r.Context(), -1, active)
			// Fixed fields only: never bodies, query strings, tokens, email or error text.
			if path != "/healthz" && path != "/readyz" && !strings.Contains(path, "/health") && !strings.Contains(path, "/readyz") {
				t.event(r.Context(), "http.request.completed", otellog.String("request_id", id), otellog.String("http.route", path), otellog.String("http.request.method", m), otellog.Int("http.response.status_code", status), otellog.Float64("duration_seconds", elapsed))
			}
		}()
		next.ServeHTTP(out, r)
		finished = true
	}), t.gateway)
}
