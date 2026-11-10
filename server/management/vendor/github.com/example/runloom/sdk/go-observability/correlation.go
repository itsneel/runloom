package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
)

// RequestIDHeader is diagnostic metadata, never an authentication credential.
const RequestIDHeader = "X-Runloom-Request-ID"

type requestIDKey struct{}

// RequestID returns the correlation ID established by the inbound middleware.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func validRequestID(id string) bool {
	if len(id) != 32 || id == "00000000000000000000000000000000" {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

func correlate(next http.Handler, gateway bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		// A public gateway starts a new chain. Direct service ingress accepts
		// only canonical IDs; these remain untrusted diagnostic metadata.
		if values := r.Header.Values(RequestIDHeader); !gateway && len(values) == 1 && validRequestID(values[0]) {
			id = values[0]
		}
		if id == "" {
			id = newRequestID()
		}
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		r.Header = r.Header.Clone()
		r.Header.Set(RequestIDHeader, id)
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}

type correlationTransport struct {
	base   http.RoundTripper
	scheme string
	host   string
}

// InternalTransport propagates correlation only to the exact configured Runloom
// origin. It must not be installed globally or on provider/customer API clients.
// Redirects to any other origin have this header removed. Requests and their
// headers are cloned so concurrent callers never modify one another's metadata.
func InternalTransport(base http.RoundTripper, origin string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	t := &correlationTransport{base: base}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && u.User == nil &&
		(u.Scheme == "http" || u.Scheme == "https") && u.RawQuery == "" && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/") {
		t.scheme, t.host = u.Scheme, u.Host
	}
	return t
}

func (t *correlationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	out.Header.Del(RequestIDHeader)
	if t.host != "" && out.URL.Scheme == t.scheme && out.URL.Host == t.host && out.URL.User == nil {
		id := RequestID(out.Context())
		if id == "" {
			id = newRequestID()
		}
		out.Header.Set(RequestIDHeader, id)
	}
	return t.base.RoundTrip(out)
}
