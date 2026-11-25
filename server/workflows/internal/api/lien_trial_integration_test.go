package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"
)

// A local integration trial, not an R2/classifier/QA or authentication E2E test.
// Runloom API handlers, file store, interpreter and Temporal are real. Identity
// verification uses Runloom's existing test fixture. Business calls use loopback
// HTTP adapters and synthetic payloads; no production worker code is modified.
func TestLienTagTrialLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	out := os.Getenv("LIEN_TRIAL_OUTPUT")
	if address == "" || out == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS and LIEN_TRIAL_OUTPUT")
	}
	require.NoError(t, os.MkdirAll(out, 0755))
	write := func(name string, value any) {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(out, name), append(data, '\n'), 0644))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	queue := "lien-trial-" + uuid.NewString()
	f := newConsumerFixtureClient(t, c, queue)
	api := httptest.NewServer(f.g)
	defer api.Close()
	var apiJournal []map[string]any
	call := func(method, path, token, key string, body any, status int) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r, err := http.NewRequestWithContext(ctx, method, api.URL+path, bytes.NewReader(raw))
		require.NoError(t, err)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Runloom-Team", f.team)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := api.Client().Do(r)
		require.NoError(t, err)
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, string(data))
		apiJournal = append(apiJournal, map[string]any{"method": method, "path": path, "status": status, "idempotency_key": key})
		return data
	}
	var draft model.Definition
	raw, err := os.ReadFile("../../testdata/lien-trial/definition.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &draft))
	call("POST", "/api/v1/studio/definitions", "admin", "", draft, 200)
	call("POST", "/api/v1/studio/definitions/"+draft.ID+"/publish", "admin", "", map[string]any{}, 200)
	tenant, err := f.g.tenant(ctx, f.team)
	require.NoError(t, err)
	version := 1
	published, err := tenant.backend.Get(draft.ID, &version)
	require.NoError(t, err)
	write("definition-v1.json", published)
	a := f.account()
	require.NoError(t, f.directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
		account := s.ServiceAccounts[a.ClientID]
		account.Grants = []consumer.Grant{{DefinitionID: draft.ID, Versions: []int{1, 2}, OutputFields: []string{"handoffs", "saved_tags"}}}
		s.ServiceAccounts[a.ClientID] = account
		return nil
	}))

	var mu sync.Mutex
	journal := []map[string]any{}
	qaAttempts, qaEffects := map[string]int{}, map[string]int{}
	counts := map[string]int{}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		counts[r.URL.Path]++
		entry := map[string]any{"path": r.URL.Path, "body": body}
		journal = append(journal, entry)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/r2/read":
			event := body["event"].(map[string]any)
			_ = json.NewEncoder(w).Encode(map[string]any{"pdf_url": event["pdf_reference"], "tags": []string{"external"}, "trusted_roles": []map[string]string{{"tag": "sewer_bill_1", "provider_result_id": "synthetic-provider-001"}}, "manual_tag_revision": event["manual_tag_revision"]})
		case "/classifier":
			if body["order_type"] != "LIEN" || body["pdf_url"] != "fixture://lien/water-sewer.pdf" {
				http.Error(w, "incorrect classifier request", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"final_tag": "water_bill_1", "requires_human_review": false})
		case "/r2/save":
			event := body["event"].(map[string]any)
			document := body["document"].(map[string]any)
			if body["primary_tag"] != "water_bill_1" || document["trusted_roles"].([]any)[0].(map[string]any)["tag"] != "sewer_bill_1" {
				http.Error(w, "unsupported fixture tag evidence", 400)
				return
			}
			handoffs := []map[string]any{}
			for _, category := range []string{"Water", "Sewer"} {
				handoff := map[string]any{}
				for _, key := range []string{"workflow_id", "document_id", "document_version", "source_association_revision", "source_communication_id", "request_id", "attempt_id"} {
					handoff[key] = event[key]
				}
				handoff["qa_category"] = category
				handoff["document_type"] = strings.ToLower(category) + "_bill_1"
				handoff["response_date"] = event["original_response_date"]
				handoff["event_id"] = event["event_id"]
				handoff["handoff_id"] = fmt.Sprintf("%s:%s:v%v:a%v:%s", event["workflow_id"], event["document_id"], event["document_version"], event["source_association_revision"], category)
				handoffs = append(handoffs, handoff)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tags": []string{"external", "water_bill_1", "sewer_bill_1"}, "qa_handoffs": handoffs})
		case "/qa":
			handoff := body["handoff"].(map[string]any)
			key := handoff["handoff_id"].(string)
			qaAttempts[key]++
			if qaEffects[key] == 0 {
				qaEffects[key] = 1 // Fixture QA receiver owns business idempotency.
			}
			entry["handoff_id"], entry["attempt"], entry["business_effects"] = key, qaAttempts[key], qaEffects[key]
			if handoff["event_id"] == "synthetic-lost-ack-002" && handoff["qa_category"] == "Water" && qaAttempts[key] == 1 {
				entry["acknowledgment"] = "connection dropped after acceptance"
				panic(http.ErrAbortHandler)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "handoff_id": key, "qa_category": handoff["qa_category"]})
		default:
			http.Error(w, "unknown fixture operation", 404)
		}
	}))
	defer stub.Close()
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	businessClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	adapter := func(path string) func(context.Context, map[string]any) (map[string]any, error) {
		return func(ctx context.Context, body map[string]any) (map[string]any, error) {
			raw, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			r, err := http.NewRequestWithContext(ctx, "POST", stub.URL+path, bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			r.Header.Set("Content-Type", "application/json")
			response, err := businessClient.Do(r)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				return nil, fmt.Errorf("fixture %s returned %d", path, response.StatusCode)
			}
			var result map[string]any
			err = json.NewDecoder(response.Body).Decode(&result)
			return result, err
		}
	}
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
	for name, path := range map[string]string{"trial.r2.read": "/r2/read", "trial.classifier": "/classifier", "trial.r2.save": "/r2/save", "trial.qa.submit": "/qa"} {
		w.RegisterActivityWithOptions(adapter(path), activity.RegisterOptions{Name: name})
	}
	startedWorker := false
	defer func() {
		if startedWorker {
			w.Stop()
		}
	}()
	raw, err = os.ReadFile("../../testdata/lien-trial/input.json")
	require.NoError(t, err)
	var original map[string]any
	require.NoError(t, json.Unmarshal(raw, &original))
	summary := map[string]any{"runloom_revision": "92675ef110014a7e9640737762c8284c11976da1", "temporal_address": address, "task_queue": queue, "authentication": "fixture verifier", "business_services": "loopback stubs", "qa_idempotency_owner": "fixture receiver"}
	for _, scenario := range []string{"happy", "lost-ack"} {
		event := map[string]any{}
		for key, value := range original {
			event[key] = value
		}
		if scenario == "lost-ack" {
			event["event_id"] = "synthetic-lost-ack-002"
			event["document_id"] = "44444444-4444-4444-8444-444444444444"
		}
		body := map[string]any{"definition_id": draft.ID, "version": 1, "input": event}
		key := event["event_id"].(string)
		response := call("POST", "/api/v1/runs", a.ClientID, key, body, 202)
		var started struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(response, &started))
		if !startedWorker {
			// Change the published definition while version 1 is queued.
			draft.Name = "Lien tagging trial version two"
			draft.Steps = append(draft.Steps, model.Step{ID: "version-two-only", Type: "activity", Activity: "runloom.echo", With: map[string]any{"marker": "v2"}})
			call("POST", "/api/v1/studio/definitions", "admin", "", draft, 200)
			call("POST", "/api/v1/studio/definitions/"+draft.ID+"/publish", "admin", "", map[string]any{}, 200)
			versionTwo := 2
			v2, err := tenant.backend.Get(draft.ID, &versionTwo)
			require.NoError(t, err)
			write("definition-v2.json", v2)
			require.NoError(t, w.Start())
			startedWorker = true
		}
		var result model.RunResult
		require.NoError(t, c.GetWorkflow(ctx, started.ID, "").Get(ctx, &result))
		require.Equal(t, "completed", result.Status)
		require.NotContains(t, result.Steps, "version-two-only")
		output := result.Output.(map[string]any)
		require.Len(t, output["handoffs"], 2)
		require.ElementsMatch(t, []any{"external", "water_bill_1", "sewer_bill_1"}, output["saved_tags"])
		first, err := c.DescribeWorkflowExecution(ctx, started.ID, "")
		require.NoError(t, err)
		runID := first.WorkflowExecutionInfo.Execution.RunId
		mu.Lock()
		before := len(journal)
		mu.Unlock()
		replay := call("POST", "/api/v1/runs", a.ClientID, key, body, 202)
		var replayed struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(replay, &replayed))
		require.Equal(t, started.ID, replayed.ID)
		next, err := c.DescribeWorkflowExecution(ctx, started.ID, "")
		require.NoError(t, err)
		require.Equal(t, runID, next.WorkflowExecutionInfo.Execution.RunId)
		status := call("GET", "/api/v1/runs/"+started.ID, a.ClientID, "", nil, 200)
		require.Contains(t, string(status), `"status":"completed"`)
		historyView := call("GET", "/api/v1/studio/runs/"+started.ID+"/history?trace=true", "admin", "", nil, 200)
		require.Contains(t, string(historyView), `"trace"`)
		write(scenario+"-run-history.json", historyView)
		write(scenario+"-result.json", result)
		write(scenario+"-input.json", event)
		history := &historypb.History{}
		iterator := c.GetWorkflowHistory(ctx, started.ID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		maxAttempt := int32(0)
		for iterator.HasNext() {
			event, err := iterator.Next()
			require.NoError(t, err)
			history.Events = append(history.Events, event)
			if a := event.GetActivityTaskStartedEventAttributes(); a != nil && a.Attempt > maxAttempt {
				maxAttempt = a.Attempt
			}
		}
		rawHistory, err := protojson.MarshalOptions{Indent: "  "}.Marshal(history)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(out, scenario+"-temporal-history.json"), rawHistory, 0644))
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(engine.RunloomWorkflow)
		require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
		mu.Lock()
		require.Equal(t, before, len(journal), "duplicate delivery and history replay must not call external adapters")
		for _, category := range []string{"Water", "Sewer"} {
			id := fmt.Sprintf("%s:%s:v1:a7:%s", event["workflow_id"], event["document_id"], category)
			require.Equal(t, 1, qaEffects[id])
			wantAttempts := 1
			if scenario == "lost-ack" && category == "Water" {
				wantAttempts = 2
			}
			require.Equal(t, wantAttempts, qaAttempts[id])
		}
		mu.Unlock()
		if scenario == "lost-ack" {
			require.Equal(t, int32(2), maxAttempt)
		}
		summary[scenario] = map[string]any{"workflow_id": started.ID, "run_id": runID, "definition_version": 1, "duplicate_reused_same_run": true, "history_replay_passed": true, "history_event_count": len(history.Events), "maximum_activity_attempt": maxAttempt, "qa_effects": map[string]int{"Water": 1, "Sewer": 1}}
	}
	require.Equal(t, 2, counts["/classifier"])
	require.Equal(t, 5, counts["/qa"])
	write("business-http-journal.json", journal)
	write("api-http-journal.json", apiJournal)
	write("summary.json", summary)
}
