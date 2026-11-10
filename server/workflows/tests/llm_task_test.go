package workflows_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/llmcontract"
	"github.com/stretchr/testify/require"
)

func TestLLMTaskSchemaAndReview(t *testing.T) {
	task := llmcontract.Task{Kind: "classify", Labels: []string{"routine", "escalation"}}
	for _, content := range []string{
		`{"content":"ok","data":{"label":"routine","reason":"routine"},"hitl_required":false,"hitl_reason":""}`,
		`{"content":"unknown","data":{"label":null,"reason":"unknown"},"hitl_required":true,"hitl_reason":"Choose a category"}`,
	} {
		_, err := task.Parse(content)
		require.NoError(t, err)
	}
	for _, content := range []string{
		`{"content":"ok","data":{"label":"invented","reason":"routine"},"hitl_required":false,"hitl_reason":""}`,
		`{"content":"ok","data":{"label":null,"reason":"routine"},"hitl_required":false,"hitl_reason":""}`,
		`{"content":"ok","data":{"label":"routine"},"hitl_required":false,"hitl_reason":""}`,
		`{"content":"ok","data":{"label":"routine","reason":"ok","extra":true},"hitl_required":false,"hitl_reason":""}`,
		`{"content":"ok","data":{"label":"escalation","label":"routine","reason":"ok"},"hitl_required":false,"hitl_reason":""}`,
	} {
		_, err := task.Parse(content)
		require.Error(t, err)
	}
	extract := llmcontract.Task{Kind: "extract", Fields: []llmcontract.TaskField{{Name: "amount", Type: "number"}}}
	_, err := extract.Parse(`{"content":"ok","data":{"amount":null},"hitl_required":false,"hitl_reason":""}`)
	require.Error(t, err)
	_, err = extract.Parse(`{"content":"ok","data":{"amount":12},"hitl_required":false,"hitl_reason":""}`)
	require.NoError(t, err)
}

func TestLLMTaskSummaryAndSentimentContracts(t *testing.T) {
	summary := llmcontract.Task{Kind: "summarize"}
	_, err := summary.Parse(`{"content":"Summary","data":{"summary":"All good","key_points":["First","Second"]},"hitl_required":false,"hitl_reason":""}`)
	require.NoError(t, err)
	_, err = summary.Parse(`{"content":"Summary","data":{"summary":"All good","key_points":"not an array"},"hitl_required":false,"hitl_reason":""}`)
	require.Error(t, err)
	sentiment := llmcontract.Task{Kind: "sentiment"}
	_, err = sentiment.Parse(`{"content":"Unclear","data":{"sentiment":"unknown","reason":"Ambiguous"},"hitl_required":false,"hitl_reason":""}`)
	require.Error(t, err)
	_, err = sentiment.Parse(`{"content":"Unclear","data":{"sentiment":"unknown","reason":"Ambiguous"},"hitl_required":true,"hitl_reason":"Review the message"}`)
	require.NoError(t, err)
}

func TestLLMTaskProviderRequestAndTypedResponse(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"Positive","data":{"sentiment":"positive","reason":"Happy customer"},"hitl_required":false,"hitl_reason":""}`}}}, "usage": map[string]any{"total_tokens": 10}})
	}))
	defer server.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic-test-key")
	t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
	t.Setenv("RUNLOOM_LLM_BASE_URL", server.URL)
	out, err := activities.LLM(t.Context(), map[string]any{"model": "fixture", "system_prompt": "Analyze", "prompt": "Great service", "_runloom_llm_task": map[string]any{"kind": "sentiment"}, "temperature": 0.2, "max_tokens": 100})
	require.NoError(t, err)
	require.Equal(t, "positive", out["data"].(map[string]any)["sentiment"])
	format := received["response_format"].(map[string]any)["json_schema"].(map[string]any)
	require.Equal(t, "runloom_llm_task_v1", format["name"])
	require.Equal(t, true, format["strict"])
	require.Contains(t, out, "usage")
	require.Equal(t, false, out["hitl_required"])
}
