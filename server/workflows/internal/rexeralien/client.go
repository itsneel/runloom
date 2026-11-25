package rexeralien

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/config"
)

type Client struct {
	TeamID                                        string
	R2URL, ClassifierURL, QAURL                   string
	R2TokenFile, ClassifierTokenFile, QATokenFile string
	AllowHTTP                                     bool
	HTTP                                          *http.Client
}

func FromConfig() *Client {
	return &Client{TeamID: config.Value("RUNLOOM_LIEN_TEAM_ID"), R2URL: config.Value("RUNLOOM_LIEN_R2_URL"),
		ClassifierURL: config.Value("RUNLOOM_LIEN_CLASSIFIER_URL"), QAURL: config.Value("RUNLOOM_LIEN_QA_URL"),
		R2TokenFile: config.Value("RUNLOOM_LIEN_R2_TOKEN_FILE"), ClassifierTokenFile: config.Value("RUNLOOM_LIEN_CLASSIFIER_TOKEN_FILE"),
		QATokenFile: config.Value("RUNLOOM_LIEN_QA_TOKEN_FILE"), AllowHTTP: config.Value("RUNLOOM_LIEN_ALLOW_HTTP") == "true"}
}

type responseError struct{ Status int }

func (e *responseError) Error() string { return fmt.Sprintf("lien service returned HTTP %d", e.Status) }
func statusCode(err error) int {
	if e, ok := err.(*responseError); ok {
		return e.Status
	}
	return 0
}
func (c *Client) request(ctx context.Context, rawURL, tokenFile string, body any, key string, unwrap bool) (map[string]any, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(c.AllowHTTP && u.Scheme == "http")) {
		return nil, fmt.Errorf("lien service endpoint is not configured; HTTPS is required")
	}
	secret, err := os.ReadFile(tokenFile)
	if err != nil || len(bytes.TrimSpace(secret)) == 0 || len(secret) > 65536 {
		return nil, fmt.Errorf("lien service credential file is unavailable")
	}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > 1024*1024 {
		return nil, fmt.Errorf("lien request exceeds the payload limit")
	}
	r, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid lien service request")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := h.Do(r)
	if err != nil {
		return nil, fmt.Errorf("lien service connection failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &responseError{Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return nil, fmt.Errorf("lien service response exceeds the payload limit")
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil {
		return nil, fmt.Errorf("lien service returned invalid JSON")
	}
	if unwrap {
		if result["success"] != true {
			return nil, fmt.Errorf("R2 did not confirm the operation")
		}
		if value, ok := result["data"].(map[string]any); ok {
			return value, nil
		}
		return nil, fmt.Errorf("R2 returned an invalid operation result")
	}
	return result, nil
}
func (c *Client) R2(ctx context.Context, path string, body any) (map[string]any, error) {
	return c.request(ctx, strings.TrimRight(c.R2URL, "/")+path, c.R2TokenFile, body, "", true)
}
func (c *Client) Classifier(ctx context.Context, body any) (map[string]any, error) {
	return c.request(ctx, c.ClassifierURL, c.ClassifierTokenFile, body, "", false)
}
func (c *Client) QA(ctx context.Context, body any, id string) (map[string]any, error) {
	return c.request(ctx, c.QAURL, c.QATokenFile, body, id, false)
}
