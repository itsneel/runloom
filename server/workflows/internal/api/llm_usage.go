package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
)

func usageSummary(ctx context.Context, b store.Metering, run store.RunRecord) (usage.Summary, error) {
	key := usage.RunKey(run.WorkflowID, run.TemporalRunID)
	entries, err := b.QueryUsage(ctx, usage.Filter{RunKey: key, Limit: 10001})
	if err != nil {
		return usage.Summary{}, err
	}
	truncated := len(entries) > 10000
	if truncated {
		entries = entries[:10000]
	}
	for i := range entries {
		markStaleUsage(&entries[i], run)
	}
	summary := usage.Summarize(entries, key, truncated)
	if len(entries) == 0 {
		summary.Complete = false
	} // historical/uninstrumented is not zero spend
	return summary, nil
}

func markStaleUsage(entry *usage.Entry, run store.RunRecord) {
	if entry.Accounting != "pending" {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, entry.StartedAt)
	if runStatusFinished(run.TemporalStatus) || (err == nil && time.Since(at) > time.Hour) {
		entry.Accounting = "unknown"
	}
}

func (s *Server) runLLMUsage(w http.ResponseWriter, r *http.Request) {
	run, err := s.modules.Runs.GetRun(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	limit, ok := parseLimit(w, r, 50, 100)
	if !ok {
		return
	}
	query := r.URL.Query()
	after := query.Get("after")
	if after != "" {
		if _, err := uuid.Parse(after); err != nil {
			writeError(w, 400, fmt.Errorf("invalid usage cursor"))
			return
		}
	}
	if len(query.Get("model")) > 200 || len(query.Get("step_id")) > 100 {
		writeError(w, 400, fmt.Errorf("invalid usage filter"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	summary, err := usageSummary(ctx, s.modules.Metering, run)
	if err != nil {
		writeError(w, 503, fmt.Errorf("usage accounting unavailable"))
		return
	}
	entries, err := s.modules.Metering.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey(run.WorkflowID, run.TemporalRunID), Model: query.Get("model"), StepID: query.Get("step_id"), After: after, Limit: limit + 1})
	if err != nil {
		writeError(w, 503, fmt.Errorf("usage accounting unavailable"))
		return
	}
	next := ""
	if len(entries) > limit {
		entries = entries[:limit]
		next = entries[len(entries)-1].ID
	}
	for i := range entries {
		markStaleUsage(&entries[i], run)
	}
	writeJSON(w, 200, map[string]any{"summary": summary, "entries": entries, "next_cursor": next, "currency": "USD", "scope": "run_and_descendants"})
}
