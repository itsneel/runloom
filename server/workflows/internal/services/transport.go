// Package services contains deployment composition and versioned internal API
// adapters. Domain packages never import it or connect to another owner's DB.
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	runloomauth "github.com/example/runloom/sdk/go-auth"
	observability "github.com/example/runloom/sdk/go-observability"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
)

var ErrUnavailable = errors.New("dependent service unavailable")

type Client struct {
	URL, Role, Token string
	HTTP             *http.Client
}

func NewClient(endpoint, role, tokenFile string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid service endpoint")
	}
	b, err := os.ReadFile(tokenFile)
	if err != nil || len(strings.TrimSpace(string(b))) < 32 {
		return nil, fmt.Errorf("service credential file is missing or invalid")
	}
	return &Client{URL: strings.TrimRight(endpoint, "/"), Role: role, Token: strings.TrimSpace(string(b)), HTTP: &http.Client{Transport: observability.InternalTransport(nil, endpoint), Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) call(ctx context.Context, path string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/internal/v1/"+path, bytes.NewReader(b))
	if err != nil {
		return ErrUnavailable
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("X-Runloom-Service", c.Role)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		switch resp.StatusCode {
		case 404:
			return store.ErrNotFound
		case 403:
			return access.ErrForbidden
		default:
			return ErrUnavailable
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Trust contains hashes only. Each workload receives its own token file, never
// another workload's credential. No token or internal body is logged.
type Trust map[string]string

func LoadTrust(path string) (Trust, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("service trust file unavailable")
	}
	var t Trust
	if json.Unmarshal(b, &t) != nil || len(t) == 0 {
		return nil, fmt.Errorf("invalid service trust file")
	}
	for role, h := range t {
		raw, e := hex.DecodeString(h)
		if !validRole(role) || e != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("invalid service trust entry")
		}
	}
	return t, nil
}
func validRole(role string) bool {
	return role == "auth" || role == "workflows" || role == "management" || role == "worker"
}
func (t Trust) caller(r *http.Request) string {
	role := r.Header.Get("X-Runloom-Service")
	expected, ok := t[role]
	if !ok {
		return ""
	}
	token, err := runloomauth.Bearer(r)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(hash[:])), []byte(expected)) != 1 {
		return ""
	}
	return role
}

func reply(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		code := 503
		if errors.Is(err, store.ErrNotFound) {
			code = 404
		}
		if errors.Is(err, access.ErrForbidden) {
			code = 403
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": http.StatusText(code)})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", 400)
		return false
	}
	return true
}
