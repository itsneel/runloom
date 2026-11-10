package usage

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func catalogue() Catalogue {
	return Catalogue{Rates: []Rate{{Provider: "test", Model: "model", Version: "v1", Currency: "USD", EffectiveAt: "2020-01-01T00:00:00Z", Input: "1", Output: "2", Cached: "0.5"}, {Provider: "test", Model: "model", Version: "v2", Currency: "USD", EffectiveAt: "2030-01-01T00:00:00Z", Input: "3", Output: "5"}}}
}
func TestExactPricingAndSubsets(t *testing.T) {
	c := catalogue()
	require.NoError(t, c.Validate())
	e := Entry{Provider: "test", Model: "model", StartedAt: "2026-10-01T00:00:00Z"}
	c.Price(&e, map[string]any{"prompt_tokens": json.Number("1000"), "completion_tokens": json.Number("500"), "prompt_tokens_details": map[string]any{"cached_tokens": json.Number("200")}, "completion_tokens_details": map[string]any{"reasoning_tokens": json.Number("100")}})
	require.Equal(t, "0.001900000000", e.CostUSD)
	require.Equal(t, "v1", e.Pricing.Version)
	require.Equal(t, int64(100), e.ReasoningTokens)
	s := Summarize([]Entry{e, e, e}, "", false)
	require.Equal(t, "0.005700000000", s.EstimatedUSD)
	require.True(t, s.Complete)
	for _, value := range []any{-1, 1.5, "100", json.Number("1000000000001")} {
		row := Entry{}
		c.Price(&row, map[string]any{"prompt_tokens": value, "completion_tokens": 0})
		require.Equal(t, "unknown", row.Accounting)
		require.Empty(t, row.CostUSD)
	}
	row := Entry{Provider: "test", Model: "missing", StartedAt: e.StartedAt}
	c.Price(&row, map[string]any{"prompt_tokens": 100, "completion_tokens": 5})
	require.Equal(t, "unpriced", row.Accounting)
	row = Entry{Provider: "test", Model: "model", StartedAt: e.StartedAt}
	c.Price(&row, map[string]any{"total_tokens": 100})
	require.Equal(t, "unknown", row.Accounting)
	row = Entry{Provider: "test", Model: "model", StartedAt: e.StartedAt}
	c.Price(&row, map[string]any{"prompt_tokens": 100, "completion_tokens": 5, "prompt_tokens_details": map[string]any{"cached_tokens": 101}})
	require.Equal(t, "unknown", row.Accounting)
}
func TestParentTotalsAndIncomplete(t *testing.T) {
	entries := []Entry{{WorkflowID: "parent", TemporalRunID: "p", StepID: "a", Accounting: "estimated", CostUSD: "0.1"}, {WorkflowID: "child", TemporalRunID: "c", StepID: "a", Accounting: "estimated", CostUSD: "0.2"}, {WorkflowID: "child", TemporalRunID: "c", StepID: "a", Accounting: "unknown"}}
	s := Summarize(entries, RunKey("parent", "p"), false)
	require.Equal(t, "0.300000000000", s.EstimatedUSD)
	require.Equal(t, "0.100000000000", s.DirectUSD)
	require.Equal(t, "0.200000000000", s.ChildUSD)
	require.Equal(t, 1, s.Unknown)
	require.False(t, s.Complete)
	require.Len(t, s.Nodes, 2)
	for _, rate := range []string{"-1", "NaN", "1e2", "0.0000001"} {
		c := catalogue()
		c.Rates[0].Input = rate
		require.Error(t, c.Validate())
	}
}
