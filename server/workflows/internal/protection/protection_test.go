package protection

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func TestEncryptionRotationAuthenticationAndNoDowngrade(t *testing.T) {
	old, err := New("old", map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)})
	require.NoError(t, err)
	raw := []byte(`{"patient":"synthetic-sensitive-marker"}`)
	encrypted, err := old.Seal("run/a/input", raw)
	require.NoError(t, err)
	require.NotContains(t, string(encrypted), "synthetic-sensitive-marker")
	second, err := old.Seal("run/a/input", raw)
	require.NoError(t, err)
	require.NotEqual(t, string(encrypted), string(second))
	rotated, err := New("new", map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "new": bytes.Repeat([]byte{2}, 32)})
	require.NoError(t, err)
	decoded, err := rotated.Open("run/a/input", encrypted)
	require.NoError(t, err)
	require.Equal(t, raw, decoded)
	_, err = rotated.Open("run/b/input", encrypted)
	require.ErrorIs(t, err, ErrProtectedData)
	_, err = rotated.Open("run/a/input", raw)
	require.ErrorIs(t, err, ErrProtectedData)
	var envelope envelope
	require.NoError(t, json.Unmarshal(encrypted, &envelope))
	envelope.Data[0] ^= 1
	damaged, _ := json.Marshal(envelope)
	_, err = rotated.Open("run/a/input", damaged)
	require.ErrorIs(t, err, ErrProtectedData)
	missing, err := New("new", map[string][]byte{"new": bytes.Repeat([]byte{2}, 32)})
	require.NoError(t, err)
	_, err = missing.Open("run/a/input", encrypted)
	require.ErrorIs(t, err, ErrProtectedData)
	t.Setenv("RUNLOOM_ALLOW_LEGACY_PAYLOADS", "true")
	decoded, err = rotated.Open("run/a/input", raw)
	require.NoError(t, err)
	require.Equal(t, raw, decoded)
	// Compatibility never bypasses ciphertext authentication.
	_, err = rotated.Open("run/a/input", damaged)
	require.ErrorIs(t, err, ErrProtectedData)
}

func TestFailureAttributesAndSafeLogging(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(key, bytes.Repeat([]byte{7}, 32), 0600))
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", key)
	opts, err := TemporalOptions("localhost:7233", "default")
	require.NoError(t, err)
	failure := opts.FailureConverter.ErrorToFailure(fmt.Errorf("synthetic-private-failure"))
	require.NotContains(t, failure.Message, "synthetic-private-failure")
	require.Equal(t, encoding, string(failure.EncodedAttributes.Metadata[converter.MetadataEncoding]))
	require.NotContains(t, string(failure.EncodedAttributes.Data), "synthetic-private-failure")
	require.Contains(t, opts.FailureConverter.FailureToError(failure).Error(), "synthetic-private-failure")
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	SafeLogger{}.Error("synthetic-private-failure", "token", "synthetic-secret")
	require.Contains(t, output.String(), "temporal_sdk level=error")
	require.NotContains(t, output.String(), "synthetic")
}

func TestTLSVerifiesPrivateCAAndHostname(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	t.Setenv("TEMPORAL_TLS_CA_FILE", ca)
	settings, err := temporalTLS()
	require.NoError(t, err)
	require.False(t, settings.InsecureSkipVerify)
	transport := &http.Transport{TLSClientConfig: settings}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Get(server.URL)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, 204, response.StatusCode)
	t.Setenv("TEMPORAL_TLS_SERVER_NAME", "wrong.example.test")
	settings, err = temporalTLS()
	require.NoError(t, err)
	bad := &http.Transport{TLSClientConfig: settings}
	defer bad.CloseIdleConnections()
	_, err = (&http.Client{Transport: bad}).Get(server.URL)
	require.Error(t, err)
	t.Setenv("TEMPORAL_TLS_KEY_FILE", "/does-not-exist")
	_, err = temporalTLS()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "does-not-exist")
}

func TestDatabaseRuntimeRequiresEncryption(t *testing.T) {
	t.Setenv("RUNLOOM_DATABASE_DRIVER", "postgres")
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", "")
	t.Setenv("RUNLOOM_PAYLOAD_KEYRING_FILE", "")
	_, err := TemporalOptions("localhost:7233", "default")
	require.ErrorIs(t, err, ErrProtectedData)
}

func TestTemporalCodecRoundTrip(t *testing.T) {
	ring, err := New("test", map[string][]byte{"test": bytes.Repeat([]byte{3}, 32)})
	require.NoError(t, err)
	dc := converter.NewCodecDataConverter(converter.GetDefaultDataConverter(), Codec{ring})
	payload, err := dc.ToPayload(map[string]string{"value": "private-test-payload"})
	require.NoError(t, err)
	require.NotContains(t, string(payload.Data), "private-test-payload")
	var result map[string]string
	require.NoError(t, dc.FromPayload(payload, &result))
	require.Equal(t, "private-test-payload", result["value"])
	plain, err := converter.GetDefaultDataConverter().ToPayload(result)
	require.NoError(t, err)
	_, err = (Codec{ring}).Decode([]*commonpb.Payload{plain})
	require.ErrorIs(t, err, ErrProtectedData)
	payload.Data = []byte("damaged")
	require.ErrorIs(t, dc.FromPayload(payload, &result), ErrProtectedData)
}
