package activities

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

type httpTransport func(*http.Request) (*http.Response, error)

func (f httpTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testSecretResolver struct{}

func (testSecretResolver) Resolve(_ context.Context, team, name string) (string, error) {
	if team != "team-a" || name != "api-key" {
		return "", errors.New("denied")
	}
	return "synthetic-test-value", nil
}
func TestHTTPSecretIsolationAndRedaction(t *testing.T) {
	a := &HTTP{Secrets: testSecretResolver{}}
	input := model.HTTPActivityInput{TeamID: "team-a", Request: model.HTTPRequest{URL: "https://api.example.com/test", Method: "POST", Body: map[string]any{"hello": "world"}, SecretHeaders: map[string]model.SecretHeader{"Authorization": {Secret: "api-key", Prefix: "Bearer "}}}}
	calls := 0
	client := &http.Client{Transport: httpTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "Bearer synthetic-test-value", r.Header.Get("Authorization"))
		require.Equal(t, "POST", r.Method)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"echo":"synthetic-test-value","token":"anything","nested":["synthetic-test-value"]}`)), Header: make(http.Header)}, nil
	})}
	out, err := a.call(context.Background(), input, client)
	require.NoError(t, err)
	raw, _ := json.Marshal(out)
	require.NotContains(t, string(raw), "synthetic-test-value")
	require.NotContains(t, string(raw), "anything")
	require.Contains(t, string(raw), `"ok":true`)
	input.TeamID = "team-b"
	_, err = a.call(context.Background(), input, client)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	input.TeamID = "team-a"
	input.Request.URL = "https://another-api.example.com"
	_, err = a.call(context.Background(), input, client)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestHTTPNetworkPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "192.0.2.1", "0.0.0.0"} {
		require.False(t, publicIP(netip.MustParseAddr(raw)), raw)
	}
	require.True(t, publicIP(netip.MustParseAddr("8.8.8.8")))
	_, err := publicDial(context.Background(), "tcp", "127.0.0.1:443")
	require.ErrorContains(t, err, "not a public")
	_, err = (&HTTP{}).Call(context.Background(), model.HTTPActivityInput{Request: model.HTTPRequest{URL: "https://127.0.0.1/secret", Method: "GET"}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "/secret")
}

func TestHTTPErrorsNeverIncludeProviderBodyOrURL(t *testing.T) {
	for _, status := range []int{302, 400, 401, 429, 500} {
		client := &http.Client{Transport: httpTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("sensitive response")), Header: make(http.Header)}, nil
		})}
		_, err := (&HTTP{}).call(context.Background(), model.HTTPActivityInput{Request: model.HTTPRequest{URL: "https://api.example.com/?sensitive=query", Method: "GET"}}, client)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "sensitive")
		var app *temporal.ApplicationError
		if status < 500 && status != 429 {
			require.ErrorAs(t, err, &app)
			require.True(t, app.NonRetryable())
		}
	}
	client := &http.Client{Transport: httpTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 256*1024+1))), Header: make(http.Header)}, nil
	})}
	_, err := (&HTTP{}).call(context.Background(), model.HTTPActivityInput{Request: model.HTTPRequest{URL: "https://api.example.com/", Method: "GET"}}, client)
	require.ErrorContains(t, err, "256 KiB")
}
