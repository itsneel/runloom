package activities

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecisionProviderContract(t *testing.T) {
	for _, tc := range []struct {
		name, content, finish string
		refusal               any
		valid                 bool
	}{
		{"valid", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Selected"}`, "stop", nil, true},
		{"unknown", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["bad"],"reason":"Selected"}`, "stop", nil, false},
		{"truncated", `{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`, "length", nil, false},
		{"refusal", `{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`, "stop", "Refused", false},
		{"text", "Sure, choose a", "stop", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				format := body["response_format"].(map[string]any)
				require.Equal(t, "json_schema", format["type"])
				schema := format["json_schema"].(map[string]any)
				require.Equal(t, true, schema["strict"])
				require.Contains(t, body["messages"].([]any)[1].(map[string]any)["content"], "Action A")
				_ = json.NewEncoder(w).Encode(map[string]any{"model": "test", "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}, "choices": []any{map[string]any{"finish_reason": tc.finish, "message": map[string]any{"content": tc.content, "refusal": tc.refusal}}}})
			}))
			defer s.Close()
			t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
			t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic")
			t.Setenv("RUNLOOM_LLM_BASE_URL", s.URL)
			observed := false
			out, err := callLLM(t.Context(), map[string]any{"model": "test", "system_prompt": "Route", "prompt": "Request", "_runloom_decision": map[string]any{"mode": "single", "remaining": 1, "actions": []any{map[string]any{"id": "a", "description": "Action A"}}, "history": []any{}}}, func(_ context.Context, _ string) (func(llmObservation) bool, error) {
				return func(o llmObservation) bool {
					observed = true
					require.NotNil(t, o.Response["usage"])
					require.Equal(t, tc.valid, o.Success)
					return true
				}, nil
			})
			require.True(t, observed, "paid failures must still be accounted")
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.content, out["content"])
			} else {
				require.Error(t, err)
				require.Nil(t, out)
			}
		})
	}
}
