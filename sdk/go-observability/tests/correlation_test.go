package observability_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	obs "github.com/example/runloom/sdk/go-observability"
)

const callerID = "0123456789abcdef0123456789abcdef"

func middleware(t *testing.T, service string, next http.Handler) http.Handler {
	t.Helper()
	telemetry, err := obs.Start(context.Background(), service, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(telemetry.Close)
	return telemetry.Wrap(next)
}

func TestCorrelationIngressValidation(t *testing.T) {
	for _, tc := range []struct {
		name, service string
		values        []string
		preserve      bool
	}{
		{"internal", "assistant", []string{callerID}, true},
		{"gateway", "gateway", []string{callerID}, false},
		{"missing", "auth", nil, false},
		{"duplicate", "auth", []string{callerID, callerID}, false},
		{"payload", "auth", []string{"private@example.test"}, false},
		{"uppercase", "auth", []string{strings.ToUpper(callerID)}, false},
		{"zero", "auth", []string{strings.Repeat("0", 32)}, false},
		{"oversized", "auth", []string{strings.Repeat("a", 4096)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			for _, value := range tc.values {
				r.Header.Add(obs.RequestIDHeader, value)
			}
			var id string
			h := middleware(t, tc.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id = obs.RequestID(r.Context())
				if r.Header.Get(obs.RequestIDHeader) != id {
					t.Error("context/header mismatch")
				}
				w.WriteHeader(204)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if len(id) != 32 || w.Header().Get(obs.RequestIDHeader) != id || w.Code != 204 {
				t.Fatal("missing correlation or changed response")
			}
			if (id == callerID) != tc.preserve {
				t.Fatal("wrong ingress trust behavior")
			}
			if len(r.Header.Values(obs.RequestIDHeader)) != len(tc.values) {
				t.Fatal("modified the caller's header map")
			}
		})
	}
}

func TestCorrelationAcrossConcurrentThreeHopRequests(t *testing.T) {
	auth := httptest.NewServer(middleware(t, "auth", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, obs.RequestID(r.Context()))
	})))
	defer auth.Close()
	proxy := func(base string) http.Handler {
		client := &http.Client{Transport: obs.InternalTransport(nil, base), Timeout: 3 * time.Second}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, _ := http.NewRequestWithContext(r.Context(), "GET", base, nil)
			res, err := client.Do(req)
			if err != nil {
				t.Error(err)
				w.WriteHeader(502)
				return
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if string(body) != obs.RequestID(r.Context()) || res.Header.Get(obs.RequestIDHeader) != string(body) {
				t.Error("lost correlation across an internal hop")
			}
			_, _ = w.Write(body)
		})
	}
	assistant := httptest.NewServer(middleware(t, "assistant", proxy(auth.URL)))
	defer assistant.Close()
	gateway := httptest.NewServer(middleware(t, "gateway", proxy(assistant.URL)))
	defer gateway.Close()
	var wg sync.WaitGroup
	var seen sync.Map
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", gateway.URL, nil)
			req.Header.Set(obs.RequestIDHeader, callerID)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			id := res.Header.Get(obs.RequestIDHeader)
			if len(id) != 32 || id == callerID || id != string(body) {
				t.Error("gateway did not establish an independent request ID")
			}
			if _, duplicate := seen.LoadOrStore(id, true); duplicate {
				t.Error("concurrent requests shared an ID")
			}
		}()
	}
	wg.Wait()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportDoesNotLeakOrMutateHeaders(t *testing.T) {
	for _, destination := range []string{"https://workflows.test", "https://external.test", "http://workflows.test", "https://workflows.test:8443"} {
		t.Run(destination, func(t *testing.T) {
			h := middleware(t, "assistant", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out, _ := http.NewRequestWithContext(r.Context(), "GET", destination, nil)
				out.Header.Set(obs.RequestIDHeader, "must-be-replaced")
				transport := obs.InternalTransport(roundTripFunc(func(actual *http.Request) (*http.Response, error) {
					got := actual.Header.Get(obs.RequestIDHeader)
					if destination == "https://workflows.test" {
						if got != callerID {
							t.Error("internal destination did not get context ID")
						}
					} else if got != "" {
						t.Error("ID leaked across origin boundary")
					}
					return &http.Response{StatusCode: 204, Body: http.NoBody, Header: make(http.Header)}, nil
				}), "https://workflows.test")
				res, err := transport.RoundTrip(out)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				if out.Header.Get(obs.RequestIDHeader) != "must-be-replaced" {
					t.Error("transport mutated caller's request")
				}
			}))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set(obs.RequestIDHeader, callerID)
			h.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}

func TestCrossOriginRedirectStripsCorrelation(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(obs.RequestIDHeader) != "" {
			t.Error("redirect leaked correlation ID")
		}
	}))
	defer external.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Get(obs.RequestIDHeader)) != 32 {
			t.Error("background internal request missing ID")
		}
		http.Redirect(w, r, external.URL, http.StatusFound)
	}))
	defer internal.Close()
	client := &http.Client{Transport: obs.InternalTransport(nil, internal.URL), Timeout: 3 * time.Second}
	res, err := client.Get(internal.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func TestCorrelationKeepsSSEStreaming(t *testing.T) {
	release := make(chan struct{})
	sent := make(chan struct{})
	server := httptest.NewServer(middleware(t, "assistant", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		close(sent)
		<-release
	})))
	defer server.Close()
	defer close(release)
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	<-sent
	if len(res.Header.Get(obs.RequestIDHeader)) != 32 {
		t.Fatal("stream missing correlation header")
	}
	buf := make([]byte, len("data: first\n\n"))
	if _, err := io.ReadFull(res.Body, buf); err != nil || string(buf) != "data: first\n\n" {
		t.Fatal("response was buffered instead of streamed")
	}
}
