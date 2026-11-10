package consumer

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
)

// Provisioning credentials never leave the API. The application does not retrieve
// client secrets: it generates new ones and sends them to Keycloak only on writes.
type Provider interface {
	Create(context.Context, string, string, string, []string) (string, string, error)
	Rotate(context.Context, string, string, string, []string) error
	Disable(context.Context, string) error
}
type Keycloak struct {
	BaseURL, Realm, ClientID, SecretFile, Audience string
	HTTP                                           *http.Client
}

func (k *Keycloak) httpClient() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (k *Keycloak) token(ctx context.Context) (string, error) {
	raw, err := os.ReadFile(k.SecretFile)
	if err != nil {
		return "", fmt.Errorf("identity provisioning credentials unavailable")
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {k.ClientID}, "client_secret": {strings.TrimSpace(string(raw))}}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(k.BaseURL, "/")+"/realms/"+url.PathEscape(k.Realm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("invalid identity configuration")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := k.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("identity provider unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.AccessToken == "" {
		return "", fmt.Errorf("identity provisioning authentication failed")
	}
	return result.AccessToken, nil
}
func (k *Keycloak) call(ctx context.Context, method, path string, body any, out any) (http.Header, error) {
	token, err := k.token(ctx)
	if err != nil {
		return nil, err
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(k.BaseURL, "/")+"/admin/realms/"+url.PathEscape(k.Realm)+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid identity configuration")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("identity provider unavailable")
	}
	defer resp.Body.Close()
	// Never propagate a provider body: it may contain credentials or bearer tokens.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("identity provisioning failed (HTTP %d)", resp.StatusCode)
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out); err != nil {
			return nil, fmt.Errorf("invalid identity response")
		}
	}
	return resp.Header, nil
}
func (k *Keycloak) mappers(generation string) []map[string]any {
	hard := func(name, value string) map[string]any {
		return map[string]any{"name": name, "protocol": "openid-connect", "protocolMapper": "oidc-hardcoded-claim-mapper", "config": map[string]string{"claim.name": name, "claim.value": value, "jsonType.label": "String", "access.token.claim": "true", "id.token.claim": "false", "userinfo.token.claim": "false"}}
	}
	return []map[string]any{{"name": "execution-audience", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper", "config": map[string]string{"included.custom.audience": k.Audience, "access.token.claim": "true", "id.token.claim": "false"}}, hard("runloom_identity", "service_account"), hard("runloom_generation", generation)}
}
func (k *Keycloak) Create(ctx context.Context, id, generation, secret string, scopes []string) (string, string, error) {
	// Scopes are created by the operator, not by arbitrary consumer requests.
	var available []struct {
		Name string `json:"name"`
	}
	if _, err := k.call(ctx, "GET", "/client-scopes", nil, &available); err != nil {
		return "", "", err
	}
	for _, scope := range scopes {
		found := false
		for _, a := range available {
			if a.Name == scope {
				found = true
			}
		}
		if !found {
			return "", "", fmt.Errorf("execution scopes are not configured in Keycloak")
		}
	}
	body := map[string]any{"clientId": id, "enabled": true, "protocol": "openid-connect", "publicClient": false, "clientAuthenticatorType": "client-secret", "secret": secret, "serviceAccountsEnabled": true, "standardFlowEnabled": false, "implicitFlowEnabled": false, "directAccessGrantsEnabled": false, "fullScopeAllowed": false, "defaultClientScopes": append([]string{"basic"}, scopes...), "optionalClientScopes": []string{}, "protocolMappers": k.mappers(generation), "attributes": map[string]string{"access.token.lifespan": "300", "client_credentials.use_refresh_token": "false"}}
	headers, err := k.call(ctx, "POST", "/clients", body, nil)
	if err != nil {
		return "", "", err
	}
	location := headers.Get("Location")
	parts := strings.Split(strings.TrimRight(location, "/"), "/")
	providerID := parts[len(parts)-1]
	if providerID == "" {
		return "", "", fmt.Errorf("identity provider omitted client identity")
	}
	var user struct {
		ID string `json:"id"`
	}
	_, err = k.call(ctx, "GET", "/clients/"+url.PathEscape(providerID)+"/service-account-user", nil, &user)
	if err != nil || user.ID == "" {
		return providerID, "", fmt.Errorf("could not bind service account identity")
	}
	return providerID, user.ID, nil
}
func (k *Keycloak) Rotate(ctx context.Context, id, generation, secret string, scopes []string) error {
	path := "/clients/" + url.PathEscape(id)
	if _, err := k.call(ctx, "PUT", path, map[string]any{"secret": secret, "enabled": true}, nil); err != nil {
		return err
	}
	var mappers []map[string]any
	if _, err := k.call(ctx, "GET", path+"/protocol-mappers/models", nil, &mappers); err != nil {
		return err
	}
	for _, mapper := range mappers {
		if mapper["name"] == "runloom_generation" {
			mapperID, ok := mapper["id"].(string)
			if !ok {
				return fmt.Errorf("identity mapper missing ID")
			}
			replacement := k.mappers(generation)[2]
			replacement["id"] = mapperID
			_, err := k.call(ctx, "PUT", path+"/protocol-mappers/models/"+url.PathEscape(mapperID), replacement, nil)
			return err
		}
	}
	return fmt.Errorf("identity generation mapper missing")
}
func (k *Keycloak) Disable(ctx context.Context, id string) error {
	_, err := k.call(ctx, "PUT", "/clients/"+url.PathEscape(id), map[string]any{"enabled": false}, nil)
	return err
}
