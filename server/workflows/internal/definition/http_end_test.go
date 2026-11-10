package definition

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestHTTPAndEndValidation(t *testing.T) {
	d := model.Definition{SchemaVersion: "1.0", ID: "api-test", Name: "API test", Steps: []model.Step{{ID: "call", Type: "http", HTTP: &model.HTTPRequest{URL: "https://api.example.com/items/${input.id}", Method: "POST", SecretHeaders: map[string]model.SecretHeader{"Authorization": {Secret: "api-key", Prefix: "Bearer "}}}}, {ID: "end", Type: "end", With: map[string]any{}}}}
	n, err := Normalize(d)
	require.NoError(t, err)
	require.Equal(t, 1, n.Steps[0].Retry.MaximumAttempts)
	for _, raw := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com:8443", "https://${input.host}/", "https://example.com/#fragment"} {
		_, err := HTTPOrigin(raw)
		require.Error(t, err, raw)
	}
	d.Steps = append(d.Steps, model.Step{ID: "after", Type: "wait", Seconds: 1})
	_, err = Normalize(d)
	require.ErrorContains(t, err, "unreachable")
	d.Steps = []model.Step{{ID: "parallel", Type: "parallel", Branches: []model.Branch{{ID: "a", Steps: []model.Step{{ID: "end", Type: "end"}}}, {ID: "b", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 1}}}}}}
	_, err = Normalize(d)
	require.ErrorContains(t, err, "parallel join")
	h := model.HTTPRequest{URL: "https://api.example.com", Method: "GET"}
	for _, headers := range []map[string]string{{"Authorization": "literal"}, {"X-Api-Key": "literal"}, {"Host": "evil.com"}, {"X-Test": "bad\r\nInjected: yes"}, {"accept": "one", "Accept": "two"}} {
		h.Headers = headers
		require.Error(t, ValidateHTTP(&h))
	}
	h.Headers = nil
	h.SecretHeaders = map[string]model.SecretHeader{"Authorization": {Secret: "../other-team"}}
	require.Error(t, ValidateHTTP(&h))
}
