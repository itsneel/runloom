package access

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
	"sync"
	"time"
)

// KeycloakDirectory owns no identity database. Native organizations and their
// members are read from Keycloak; attributes carry Runloom hierarchy and roles.
type KeycloakDirectory struct {
	BaseURL, Realm, ClientID, SecretFile string
	HTTP                                 *http.Client
	mu                                   sync.Mutex
	token                                string
	expires                              time.Time
}
type identityHTTPError struct{ Status int }

func (e *identityHTTPError) Error() string {
	return fmt.Sprintf("Keycloak directory operation failed (HTTP %d); provider response withheld", e.Status)
}
func (k *KeycloakDirectory) http() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (k *KeycloakDirectory) bearer(ctx context.Context) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Now().Before(k.expires) {
		return k.token, nil
	}
	raw, err := os.ReadFile(k.SecretFile)
	if err != nil {
		return "", fmt.Errorf("Keycloak directory credential unavailable")
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {k.ClientID}, "client_secret": {strings.TrimSpace(string(raw))}}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(k.BaseURL, "/")+"/realms/"+url.PathEscape(k.Realm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("invalid Keycloak directory configuration")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := k.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("Keycloak directory unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.Token == "" {
		return "", fmt.Errorf("Keycloak directory authentication failed")
	}
	k.token = result.Token
	k.expires = time.Now().Add(time.Duration(result.Expires-10) * time.Second)
	return k.token, nil
}
func (k *KeycloakDirectory) call(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	token, err := k.bearer(ctx)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(k.BaseURL, "/")+"/admin/realms/"+url.PathEscape(k.Realm)+path, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid Keycloak directory configuration")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Keycloak directory unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &identityHTTPError{Status: resp.StatusCode}
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
			return nil, fmt.Errorf("invalid Keycloak directory response")
		}
	}
	return resp.Header, nil
}

type nativeOrganization struct {
	ID          string              `json:"id,omitempty"`
	Name        string              `json:"name"`
	Alias       string              `json:"alias,omitempty"`
	Enabled     bool                `json:"enabled"`
	Description string              `json:"description,omitempty"`
	RedirectURL string              `json:"redirectUrl,omitempty"`
	Domains     []map[string]any    `json:"domains,omitempty"`
	Attributes  map[string][]string `json:"attributes"`
}
type nativeMember struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

func attribute(o nativeOrganization, key string) string {
	v := o.Attributes[key]
	if len(v) > 0 {
		return v[0]
	}
	return ""
}
func orgID(o nativeOrganization) string {
	if id := attribute(o, "runloom.id"); id != "" {
		return id
	}
	return o.ID
}
func (k *KeycloakDirectory) organizations(ctx context.Context) (map[string]nativeOrganization, error) {
	out := map[string]nativeOrganization{}
	for first := 0; first < 10000; first += 100 {
		var page []nativeOrganization
		_, err := k.call(ctx, "GET", fmt.Sprintf("/organizations?first=%d&max=100&briefRepresentation=false", first), nil, &page)
		if err != nil {
			return nil, err
		}
		for _, o := range page {
			if _, exists := out[orgID(o)]; exists {
				return nil, fmt.Errorf("duplicate Runloom organization identity in Keycloak")
			}
			out[orgID(o)] = o
		}
		if len(page) < 100 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("Keycloak organization directory exceeds supported page limit")
}
func (k *KeycloakDirectory) members(ctx context.Context, id string) ([]nativeMember, error) {
	out := []nativeMember{}
	for first := 0; first < 100000; first += 100 {
		var page []nativeMember
		_, err := k.call(ctx, "GET", fmt.Sprintf("/organizations/%s/members?first=%d&max=100", url.PathEscape(id), first), nil, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 100 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("Keycloak organization membership exceeds supported page limit")
}
