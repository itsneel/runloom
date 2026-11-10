package definition

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

var headerName = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

// Origin deliberately excludes credentials, fragments and non-default ports.
func HTTPOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || strings.ContainsAny(u.Host, "${}\\") {
		return "", fmt.Errorf("API URL must use HTTPS on port 443 without credentials or fragments")
	}
	return "https://" + strings.ToLower(u.Hostname()), nil
}

func ValidateHTTP(h *model.HTTPRequest) error {
	if h == nil {
		return fmt.Errorf("http configuration is required")
	}
	if _, err := HTTPOrigin(h.URL); err != nil {
		return err
	}
	if len(h.URL) > 8192 {
		return fmt.Errorf("API URL is too long")
	}
	switch h.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return fmt.Errorf("unsupported API method")
	}
	if len(h.Headers)+len(h.SecretHeaders) > 32 {
		return fmt.Errorf("at most 32 API headers are allowed")
	}
	seen := map[string]bool{}
	check := func(name, value string) error {
		key := http.CanonicalHeaderKey(name)
		if !headerName.MatchString(name) || len(name) > 100 || strings.ContainsAny(value, "\r\n\x00") || len(value) > 8192 || seen[key] {
			return fmt.Errorf("invalid or duplicate API header")
		}
		seen[key] = true
		switch strings.ToLower(name) {
		case "host", "connection", "content-length", "transfer-encoding", "proxy-authorization", "proxy-connection", "trailer", "upgrade", "te":
			return fmt.Errorf("reserved API header")
		}
		return nil
	}
	for name, value := range h.Headers {
		if err := check(name, value); err != nil {
			return err
		}
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "cookie" || strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return fmt.Errorf("credential headers must use secret_headers")
		}
	}
	for name, value := range h.SecretHeaders {
		if err := check(name, value.Prefix); err != nil {
			return err
		}
		if !idPattern.MatchString(value.Secret) || (value.Prefix != "" && value.Prefix != "Bearer " && value.Prefix != "Basic ") {
			return fmt.Errorf("invalid secret reference or prefix")
		}
	}
	return nil
}
