package healthcheck_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/runloom/assistant/internal/healthcheck"
)

func TestArguments(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		handled bool
		code    int
	}{
		{nil, false, 0}, {[]string{"serve"}, false, 0},
		{[]string{"healthcheck"}, true, 2},
		{[]string{"healthcheck", "http://unused", "extra"}, true, 2},
		{[]string{"healthcheck", "://invalid"}, true, 1},
	} {
		handled, code := healthcheck.Handle(tc.args)
		if handled != tc.handled || code != tc.code {
			t.Errorf("arguments: got (%v, %d), want (%v, %d)", handled, code, tc.handled, tc.code)
		}
	}
}

func TestHTTPResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/ok", 0}, {"/redirect", 0}, {"/empty", 1}, {"/unready", 1},
	} {
		handled, code := healthcheck.Handle([]string{"healthcheck", server.URL + tc.path})
		if !handled || code != tc.code {
			t.Errorf("%s: handled=%v code=%d, want %d", tc.path, handled, code, tc.code)
		}
	}
}

func TestConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	if handled, code := healthcheck.Handle([]string{"healthcheck", "http://" + address}); !handled || code != 1 {
		t.Fatal("unreachable endpoint must fail")
	}
}

func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	start := time.Now()
	handled, code := healthcheck.Handle([]string{"healthcheck", server.URL})
	elapsed := time.Since(start)
	if !handled || code != 1 || elapsed < 3500*time.Millisecond || elapsed > 7*time.Second {
		t.Fatalf("expected four-second failure, got handled=%v code=%d duration=%v", handled, code, elapsed)
	}
}

func TestTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	if handled, code := healthcheck.Handle([]string{"healthcheck", server.URL}); !handled || code != 1 {
		t.Fatal("untrusted TLS certificate must fail")
	}
}
