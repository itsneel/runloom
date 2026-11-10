package services

import (
	"context"
	"fmt"
	"github.com/example/runloom/gateway/internal/api"
	"github.com/example/runloom/gateway/internal/config"
	observability "github.com/example/runloom/sdk/go-observability"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// NewGateway is a stateless router: no token, database or Temporal credentials.
// Services independently authenticate/authorize every public request.
func NewGateway() (http.Handler, error) {
	proxies := map[string]*httputil.ReverseProxy{}
	endpoints := map[string]string{}
	for role, key := range map[string]string{"auth": "RUNLOOM_AUTH_URL", "workflows": "RUNLOOM_WORKFLOWS_URL", "management": "RUNLOOM_MANAGEMENT_URL", "assistant": "RUNLOOM_ASSISTANT_URL"} {
		raw := config.Value(key)
		if role == "assistant" && raw == "" {
			continue
		}
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return nil, fmt.Errorf("invalid gateway service endpoint")
		}
		endpoints[role] = raw
		proxy := httputil.NewSingleHostReverseProxy(u)
		proxy.Transport = observability.InternalTransport(&http.Transport{ResponseHeaderTimeout: 25 * time.Second, IdleConnTimeout: 60 * time.Second}, raw)
		proxy.ModifyResponse = func(response *http.Response) error {
			// The outer middleware already set the canonical response ID.
			// ReverseProxy adds headers rather than replacing them: discard
			// the downstream copy to avoid duplicate or spoofed response IDs.
			response.Header.Del(observability.RequestIDHeader)
			return nil
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { reply(w, nil, ErrUnavailable) }
		proxies[role] = proxy
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/healthz" {
			reply(w, map[string]string{"status": "ok", "service": "gateway"}, nil)
			return
		}
		if r.URL.Path == "/readyz" || r.URL.Path == "/api/v1/health" {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			for _, endpoint := range endpoints {
				client := &http.Client{Timeout: 2 * time.Second, Transport: observability.InternalTransport(nil, endpoint), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				req, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/readyz", nil)
				resp, err := client.Do(req)
				if err != nil {
					reply(w, nil, ErrUnavailable)
					return
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					reply(w, nil, ErrUnavailable)
					return
				}
			}
			reply(w, map[string]any{"status": "ok", "services": len(endpoints), "database": true, "temporal": true}, nil)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		// Never relay caller-supplied internal identity or trusted actor headers.
		r.Header.Del("X-Runloom-Service")
		r.Header.Del("X-Runloom-Actor")
		proxy := proxies[api.RouteOwner(r.URL.Path)]
		if proxy == nil {
			reply(w, nil, ErrUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}
