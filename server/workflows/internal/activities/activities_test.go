package activities

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLLMCallsConfiguredProvider(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test-model",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"Hello from the model","hitl_required":false,"hitl_reason":""}`}}},
			"usage":   map[string]any{"total_tokens": 12},
		})
	}))
	defer server.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY", "test-key")
	t.Setenv("RUNLOOM_LLM_BASE_URL", server.URL+"/v1")

	result, err := LLM(t.Context(), map[string]any{
		"model":         "test-model",
		"system_prompt": "Be concise",
		"prompt":        "Say hello",
		"temperature":   0.2,
		"max_tokens":    100,
	})

	require.NoError(t, err)
	require.Equal(t, "Hello from the model", result["content"])
	require.Equal(t, false, result["hitl_required"])
	require.Equal(t, "", result["hitl_reason"])
	format := received["response_format"].(map[string]any)
	require.Equal(t, "json_schema", format["type"])
	require.Equal(t, true, format["json_schema"].(map[string]any)["strict"])
	require.Equal(t, "Say hello", received["messages"].([]any)[1].(map[string]any)["content"])
}
