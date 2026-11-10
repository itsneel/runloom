package consumer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Creates/removes only a new disposable Keycloak container; never uses an existing realm.
func TestKeycloakProvisioningLive(t *testing.T) {
	if os.Getenv("RUNLOOM_TEST_KEYCLOAK") != "1" {
		t.Skip("set RUNLOOM_TEST_KEYCLOAK=1 for isolated Docker identity checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	name := "runloom-consumer-test-" + uuid.NewString()
	password := uuid.NewString() + uuid.NewString()
	err := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", name, "--publish", "127.0.0.1::8080", "--env", "KC_BOOTSTRAP_ADMIN_USERNAME=bootstrap", "--env", "KC_BOOTSTRAP_ADMIN_PASSWORD="+password, "quay.io/keycloak/keycloak:26.4.7", "start-dev").Run()
	require.NoError(t, err)
	defer exec.Command("docker", "rm", "--force", name).Run()
	port, err := exec.CommandContext(ctx, "docker", "port", name, "8080/tcp").Output()
	require.NoError(t, err)
	base := "http://" + strings.TrimSpace(string(port))
	httpClient := &http.Client{Timeout: 10 * time.Second}
	token := func(realm string, form url.Values) (string, int) {
		req, err := http.NewRequestWithContext(ctx, "POST", base+"/realms/"+realm+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", 0
		}
		defer resp.Body.Close()
		var result struct {
			Token string `json:"access_token"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
		return result.Token, resp.StatusCode
	}
	adminToken := ""
	require.Eventually(t, func() bool {
		var status int
		adminToken, status = token("master", url.Values{"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"bootstrap"}, "password": {password}})
		return status == 200 && adminToken != ""
	}, 90*time.Second, time.Second)
	realm := "consumer-test"
	body, _ := json.Marshal(map[string]any{"realm": realm, "enabled": true})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/admin/realms", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 201, resp.StatusCode)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "admin-token")
	secretFile := filepath.Join(dir, "provisioner-secret")
	require.NoError(t, os.WriteFile(tokenFile, []byte(adminToken), 0600))
	setup := exec.CommandContext(ctx, "node", "../../../scripts/setup-consumer-auth.mjs", base, realm, tokenFile, secretFile)
	output, err := setup.CombinedOutput()
	require.NoError(t, err, "operator bootstrap failed: %s", string(output))
	k := &Keycloak{BaseURL: base, Realm: realm, ClientID: "runloom-provisioner", SecretFile: secretFile, Audience: "runloom-execution"}
	id, generation, secret := "runloom-sa-"+uuid.NewString(), uuid.NewString(), uuid.NewString()+uuid.NewString()
	providerID, subject, err := k.Create(ctx, id, generation, secret, Scopes)
	require.NoError(t, err)
	require.NotEmpty(t, subject)
	credentials := func(s string) url.Values {
		return url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {s}}
	}
	raw, status := token(realm, credentials(secret))
	require.Equal(t, 200, status)
	v := NewVerifier(ctx, base+"/realms/"+realm, base+"/realms/"+realm+"/protocol/openid-connect/certs", "runloom-execution")
	identity, err := v.Verify(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, id, identity.ClientID)
	require.Equal(t, subject, identity.Subject)
	require.Equal(t, generation, identity.Generation)
	for _, scope := range Scopes {
		require.Contains(t, identity.Scopes, scope)
	}
	nextGeneration, nextSecret := uuid.NewString(), uuid.NewString()+uuid.NewString()
	require.NoError(t, k.Rotate(ctx, providerID, nextGeneration, nextSecret, Scopes))
	_, status = token(realm, credentials(secret))
	require.Equal(t, 401, status, "old secret must not mint new tokens")
	raw, status = token(realm, credentials(nextSecret))
	require.Equal(t, 200, status)
	identity, err = v.Verify(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, nextGeneration, identity.Generation)
	require.NoError(t, k.Disable(ctx, providerID))
	_, status = token(realm, credentials(nextSecret))
	require.NotEqual(t, 200, status)
}
