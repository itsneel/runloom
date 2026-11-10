package activities

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
)

type SecretResolver interface {
	Resolve(context.Context, string, string) (string, error)
}
type HTTP struct{ Secrets SecretResolver }

// Explicitly reject special-use networks as well as RFC1918 and loopback.
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return true
}

func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid API address")
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("API DNS lookup failed")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("API destination is not a public address")
		}
	}
	// Dial the validated address directly: no second DNS lookup / rebinding window.
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func (a *HTTP) Call(ctx context.Context, input model.HTTPActivityInput) (map[string]any, error) {
	transport := &http.Transport{DialContext: publicDial, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true}
	defer transport.CloseIdleConnections()
	return a.call(ctx, input, &http.Client{Transport: transport, Timeout: 300 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

func (a *HTTP) call(ctx context.Context, input model.HTTPActivityInput, client *http.Client) (map[string]any, error) {
	h := input.Request
	fail := func(message string) (map[string]any, error) {
		return nil, temporal.NewNonRetryableApplicationError(message, "HTTPRequestError", nil)
	}
	if err := definition.ValidateHTTP(&h); err != nil {
		return fail(err.Error())
	}
	var body []byte
	if h.Body != nil {
		var err error
		body, err = json.Marshal(h.Body)
		if err != nil || len(body) > 256*1024 {
			return fail("API JSON body exceeds 256 KiB or is invalid")
		}
	}
	request, err := http.NewRequestWithContext(ctx, h.Method, h.URL, bytes.NewReader(body))
	if err != nil {
		return fail("invalid API request")
	}
	request.Header.Set("Accept", "application/json")
	if h.Body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range h.Headers {
		request.Header.Set(name, value)
	}
	redactions := []string{}
	for name, reference := range h.SecretHeaders {
		if a.Secrets == nil {
			return fail("workflow secrets are not configured")
		}
		value, err := a.Secrets.Resolve(ctx, input.TeamID, reference.Secret)
		if err != nil {
			return fail("secret unavailable for this team")
		}
		request.Header.Set(name, reference.Prefix+value)
		redactions = append(redactions, value, base64.StdEncoding.EncodeToString([]byte(value)), url.QueryEscape(value), url.PathEscape(value))
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("external API connection failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := fmt.Sprintf("external API returned HTTP %d", response.StatusCode)
		if response.StatusCode == 429 || response.StatusCode >= 500 {
			return nil, fmt.Errorf("%s", message)
		}
		return fail(message)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
	if err != nil || len(raw) > 256*1024 {
		return fail("API response unreadable or exceeds 256 KiB")
	}
	var data any
	if len(raw) > 0 && h.Method != "HEAD" {
		if json.Unmarshal(raw, &data) != nil {
			return fail("API response must be JSON")
		}
	}
	return map[string]any{"status_code": response.StatusCode, "body": redactHTTP(data, redactions)}, nil
}

func redactHTTP(value any, secrets []string) any {
	switch v := value.(type) {
	case string:
		for _, secret := range secrets {
			if secret != "" {
				v = strings.ReplaceAll(v, secret, "[REDACTED]")
			}
		}
		return v
	case []any:
		for i := range v {
			v[i] = redactHTTP(v[i], secrets)
		}
		return v
	case map[string]any:
		clean := map[string]any{}
		for key, item := range v {
			name := redactHTTP(key, secrets).(string)
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "api_key") {
				clean[name] = "[REDACTED]"
			} else {
				clean[name] = redactHTTP(item, secrets)
			}
		}
		return clean
	default:
		return value
	}
}
