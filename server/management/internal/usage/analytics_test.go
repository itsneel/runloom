package usage

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAnalyticsWindows(t *testing.T) {
	now := time.Date(2024, 3, 31, 12, 30, 0, 123, time.UTC)
	for period, from := range map[string]string{"1d": "2024-03-30T12:30:00Z", "1w": "2024-03-24T12:30:00Z", "1m": "2024-02-29T12:30:00Z", "mtd": "2024-03-01T00:00:00Z", "ytd": "2024-01-01T00:00:00Z", "6m": "2023-09-30T12:30:00Z", "1y": "2023-03-31T12:30:00Z"} {
		w, err := AnalyticsWindow(period, now)
		require.NoError(t, err)
		require.Equal(t, from, w.From.Format(time.RFC3339))
		require.Equal(t, w.Until.Sub(w.From), w.From.Sub(w.PreviousFrom))
		require.Zero(t, w.Until.Nanosecond())
	}
	w, err := AnalyticsWindow("1y", time.Date(2024, 2, 29, 0, 0, 0, 0, time.FixedZone("local", 19800)))
	require.NoError(t, err)
	require.Equal(t, "2023-02-28T18:30:00Z", w.From.Format(time.RFC3339))
	_, err = AnalyticsWindow("bad", now)
	require.Error(t, err)
}
func TestAnalyticsDirectAggregationAndCoverage(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	w, _ := AnalyticsWindow("1d", now)
	base := Entry{TeamID: "team", WorkflowID: "parent", TemporalRunID: "run", DefinitionID: "welcome", DefinitionVersion: 2, StartedAt: now.Add(-2 * time.Hour).Format(time.RFC3339), Accounting: "estimated", CostUSD: "0.100000000001", InputTokens: 100, OutputTokens: 50, Ancestors: []string{"parent:run"}, Attempt: 1}
	child := base
	child.WorkflowID = "child"
	child.TemporalRunID = "child-run"
	child.Ancestors = []string{"parent:run", "child:child-run"}
	child.CostUSD = "0.200000000002"
	retry := base
	retry.Attempt = 2
	retry.Status = "failed"
	unpriced := base
	unpriced.Accounting = "unpriced"
	unpriced.CostUSD = ""
	unpriced.WorkflowID = "unpriced"
	unknown := base
	unknown.Accounting = "pending"
	unknown.CostUSD = ""
	unknown.WorkflowID = "unknown"
	pending := unknown
	pending.StartedAt = now.Add(-time.Minute).Format(time.RFC3339)
	previous := base
	previous.StartedAt = w.PreviousFrom.Format(time.RFC3339)
	previous.CostUSD = "5"
	end := base
	end.StartedAt = w.Until.Format(time.RFC3339)
	boundary := base
	boundary.StartedAt = w.From.Format(time.RFC3339)
	r := Aggregate([]Entry{base, child, retry, unpriced, unknown, pending, previous, end, boundary}, map[string]string{"team": "Product", "empty": "No usage"}, w, now)
	require.Equal(t, 7, r.Summary.Requests)
	require.Equal(t, "0.500000000005", r.Summary.EstimatedUSD)
	require.Equal(t, "5.000000000000", r.Previous.EstimatedUSD)
	require.Equal(t, 4, r.Summary.Runs)
	require.Equal(t, 2, r.Summary.PricedRuns)
	require.Equal(t, "0.250000000003", *r.Summary.AverageUSD)
	require.Equal(t, 1, r.Summary.Unknown)
	require.Equal(t, 1, r.Summary.Unpriced)
	require.Equal(t, 1, r.Summary.Pending)
	require.Equal(t, 1, r.Summary.RetryRequests)
	require.Equal(t, 1, r.Summary.FailedRequests)
	require.Len(t, r.Teams, 2)
	require.Len(t, r.Trend, 24)
	empty := Aggregate(nil, map[string]string{"team": "Product"}, w, now)
	require.Nil(t, empty.Summary.AverageUSD)
	require.Zero(t, empty.Summary.Priced)
	require.Empty(t, empty.Runs)
	SortAnalytics(r.Runs, "cost", "asc")
	require.Equal(t, "0.200000000002", r.Runs[0].EstimatedUSD, "missing estimates sort last rather than appearing as free")
	SortAnalytics(r.Runs, "requests", "desc")
	require.Equal(t, 3, r.Runs[0].Requests)
}
