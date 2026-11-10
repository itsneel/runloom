package activities

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestLLMRequiredHITLContract(t *testing.T) {
	for _, tc := range []struct {
		name, content, finish string
		valid, review         bool
	}{
		{"required", `{"content":"Draft answer","hitl_required":true,"hitl_reason":"Confirm the destination"}`, "stop", true, true},
		{"not-required", `{"content":"Answer","hitl_required":false,"hitl_reason":""}`, "stop", true, false},
		{"missing", `{"content":"Answer","hitl_reason":""}`, "stop", false, false},
		{"null", `{"content":"Answer","hitl_required":null,"hitl_reason":""}`, "stop", false, false},
		{"string", `{"content":"Answer","hitl_required":"false","hitl_reason":""}`, "stop", false, false},
		{"truncated", `{"content":"Answer","hitl_required":false,"hitl_reason":""}`, "length", false, false},
		{"text", "Answer", "stop", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				schema := body["response_format"].(map[string]any)["json_schema"].(map[string]any)
				require.Equal(t, true, schema["strict"])
				require.Contains(t, schema["schema"].(map[string]any)["required"], "hitl_required")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": tc.finish, "message": map[string]any{"content": tc.content}}}, "usage": map[string]any{"prompt_tokens": 10}})
			}))
			defer server.Close()
			t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
			t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic")
			t.Setenv("RUNLOOM_LLM_BASE_URL", server.URL)
			accounted := false
			out, err := callLLM(t.Context(), map[string]any{"model": "test", "prompt": "Test", "system_prompt": ""}, func(context.Context, string) (func(llmObservation) bool, error) {
				return func(o llmObservation) bool {
					accounted = true
					require.NotNil(t, o.Response["usage"])
					require.Equal(t, tc.valid, o.Success)
					return true
				}, nil
			})
			require.True(t, accounted)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.review, out["hitl_required"])
			} else {
				var appErr *temporal.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.True(t, appErr.NonRetryable())
				require.Nil(t, out)
			}
		})
	}
}
