package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	runloomauth "github.com/example/runloom/sdk/go-auth"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

type consumerError struct {
	Status        int
	Code, Message string
}

func (e *consumerError) Error() string              { return e.Message }
func cerror(status int, code, message string) error { return &consumerError{status, code, message} }
func consumerFailure(w http.ResponseWriter, err error) {
	value := &consumerError{503, "unavailable", "Service unavailable; retry with the same idempotency key."}
	var known *consumerError
	if errors.As(err, &known) {
		value = known
	}
	if value.Status == 429 {
		w.Header().Set("Retry-After", "60")
	}
	if value.Status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="runloom-execution"`)
	}
	writeJSON(w, value.Status, map[string]any{"error": map[string]string{"code": value.Code, "message": value.Message}})
}
func (g *Gateway) ledger(ctx context.Context, id string) (consumer.Ledger, int64, error) {
	l := consumer.NewLedger()
	rev, err := g.ConsumerStore.Read(ctx, "ledger_"+id, &l)
	if errors.Is(err, consumer.ErrNotFound) {
		return l, -1, nil
	}
	return l, rev, err
}
func (g *Gateway) updateLedger(ctx context.Context, id string, fn func(*consumer.Ledger) error) error {
	for i := 0; i < 32; i++ {
		l, rev, err := g.ledger(ctx, id)
		if err != nil {
			return err
		}
		if err = fn(&l); err != nil {
			return err
		}
		ok, err := g.ConsumerStore.Swap(ctx, "ledger_"+id, rev, l)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("consumer admission contention")
}
func (g *Gateway) consumerRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Pragma", "no-cache")
	if r.Method == "GET" && r.URL.Path == "/api/v1/openapi.json" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(consumerOpenAPI)
		return
	}
	if g.ConsumerVerifier == nil || g.ConsumerStore == nil {
		consumerFailure(w, cerror(503, "not_configured", "Consumer execution is not configured."))
		return
	}
	token, tokenErr := runloomauth.Bearer(r)
	if tokenErr != nil {
		consumerFailure(w, cerror(401, "unauthorized", "Service account access token required."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	identity, err := g.ConsumerVerifier.Verify(ctx, token)
	if err != nil {
		consumerFailure(w, cerror(401, "unauthorized", "Invalid consumer access token."))
		return
	}
	directory, _, err := g.Directory.Repo.Read(ctx)
	if err != nil {
		consumerFailure(w, err)
		return
	}
	a, ok := directory.ServiceAccounts[identity.ClientID]
	if !ok || a.Status != "active" || a.Subject != identity.Subject || a.Generation != identity.Generation {
		consumerFailure(w, cerror(401, "revoked", "Service account is inactive or credentials have been rotated."))
		return
	}
	// A request cannot choose another team or impersonate a human actor.
	if team := r.Header.Get("X-Runloom-Team"); team != "" && team != a.TeamID {
		consumerFailure(w, cerror(403, "forbidden", "Service account belongs to another team."))
		return
	}
	r.Header.Set("X-Runloom-Team", a.TeamID)
	r.Header.Set("X-Runloom-Actor", "service-account:"+a.ClientID)
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	scope := ""
	switch {
	case r.Method == "GET" && (path == "workflows" || strings.HasPrefix(path, "workflows/")):
		scope = "workflows:read"
	case path == "runs" && r.Method == "POST":
		scope = "runs:start"
	case r.Method == "GET" && (path == "runs" || strings.HasPrefix(path, "runs/")):
		scope = "runs:read"
	case r.Method == "POST" && strings.HasPrefix(path, "runs/") && strings.HasSuffix(path, "/cancel"):
		scope = "runs:cancel"
	default:
		consumerFailure(w, cerror(404, "not_found", "Consumer endpoint not found."))
		return
	}
	if !slices.Contains(a.Scopes, scope) || !slices.Contains(identity.Scopes, scope) {
		consumerFailure(w, cerror(403, "insufficient_scope", "Required execution scope is not granted."))
		return
	}
	// Rate counters have their own revision scope, independent of run admission.
	err = g.updateLedger(ctx, "rate-"+a.ClientID, func(l *consumer.Ledger) error {
		minute := time.Now().Unix() / 60
		if l.Minute != minute {
			l.Minute, l.Requests = minute, 0
		}
		if l.Requests >= a.RequestsPerMinute {
			return cerror(429, "rate_limit", "Request quota exceeded.")
		}
		l.Requests++
		return nil
	})
	if err != nil {
		consumerFailure(w, err)
		return
	}
	tenant, err := g.tenant(ctx, a.TeamID)
	if err != nil {
		consumerFailure(w, err)
		return
	}
	tenant.server.auditRequest(w, r, "consumer:"+scope, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case scope == "workflows:read":
			err = g.consumerWorkflows(w, r, tenant.server, a, path)
		case path == "runs" && r.Method == "POST":
			err = g.consumerStart(w, r, tenant.server, a)
		case path == "runs":
			err = g.consumerRuns(w, r, tenant.server, a)
		default:
			err = g.consumerRun(w, r, tenant.server, a, path, scope == "runs:cancel")
		}
		if err != nil {
			consumerFailure(w, err)
		}
	}))
}
func pageOptions(r *http.Request) (int, int, error) {
	limit, after := 50, 0
	var err error
	if s := r.URL.Query().Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, cerror(422, "invalid_pagination", "limit must be 1..100")
		}
	}
	if s := r.URL.Query().Get("after"); s != "" {
		after, err = strconv.Atoi(s)
		if err != nil || after < 0 {
			return 0, 0, cerror(422, "invalid_pagination", "after must be a nonnegative cursor")
		}
	}
	return limit, after, nil
}
func (g *Gateway) consumerWorkflows(w http.ResponseWriter, r *http.Request, s *Server, a consumer.Account, path string) error {
	limit, after, err := pageOptions(r)
	if err != nil {
		return err
	}
	selected := ""
	if path != "workflows" {
		selected = strings.TrimPrefix(path, "workflows/")
		if strings.Contains(selected, "/") {
			return cerror(404, "not_found", "Workflow not found.")
		}
	}
	items := []map[string]any{}
	grants := append([]consumer.Grant(nil), a.Grants...)
	sort.Slice(grants, func(i, j int) bool { return grants[i].DefinitionID < grants[j].DefinitionID })
	for _, grant := range grants {
		if selected != "" && selected != grant.DefinitionID {
			continue
		}
		versions := append([]int(nil), grant.Versions...)
		sort.Ints(versions)
		versions = slices.Compact(versions)
		for _, v := range versions {
			d, err := s.modules.Catalog.Get(grant.DefinitionID, &v)
			if err != nil {
				return err
			}
			if d.Kind == "child" {
				continue
			}
			items = append(items, map[string]any{"id": d.ID, "name": d.Name, "version": v, "input_schema": safeSchema(d.InputSchema), "exposed_input_fields": grant.InputFields, "exposed_output_fields": grant.OutputFields})
		}
	}
	if selected != "" && len(items) == 0 {
		return cerror(404, "not_found", "Workflow not found.")
	}
	start := min(after, len(items))
	end := min(start+limit, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	writeJSON(w, 200, map[string]any{"items": items[start:end], "next_cursor": next})
	return nil
}
func safeSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if k != "default" && k != "examples" {
				out[k] = safeSchema(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = safeSchema(val)
		}
		return out
	}
	return v
}
func hashValue(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (g *Gateway) consumerStart(w http.ResponseWriter, r *http.Request, s *Server, a consumer.Account) error {
	var body struct {
		DefinitionID string         `json:"definition_id"`
		Version      int            `json:"version"`
		Input        map[string]any `json:"input"`
	}
	if decodeJSON(w, r, &body) != nil {
		return cerror(422, "invalid_request", "Provide definition_id, published version and a JSON input object.")
	}
	if body.Input == nil {
		body.Input = map[string]any{}
	}
	if _, ok := a.Grant(body.DefinitionID, body.Version); !ok {
		return cerror(403, "workflow_forbidden", "Workflow version is not granted.")
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 200 || strings.TrimSpace(key) != key {
		return cerror(422, "idempotency_key_required", "Idempotency-Key must contain 8..200 characters.")
	}
	fingerprint, keyHash := hashValue(body), hashValue(key)
	id := a.TeamID + "-consumer-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(a.ClientID+":"+keyHash)).String()
	l, _, err := g.ledger(r.Context(), a.ClientID)
	if err != nil {
		return err
	}
	if existing, ok := l.Runs[id]; ok && existing.Fingerprint != fingerprint {
		return cerror(409, "idempotency_conflict", "This key was already used for a different request.")
	}
	// Reject invalid definitions/input before reserving capacity or an idempotency key.
	value, err := s.modules.Catalog.Get(body.DefinitionID, &body.Version)
	if err != nil {
		return err
	}
	if value.Kind == "child" {
		return cerror(422, "child_only", "Child workflows require a parent.")
	}
	if definition.ValidateInput(value.InputSchema, body.Input) != nil {
		return cerror(422, "invalid_input", "Input does not match the published schema.")
	}
	prepared, err := s.prepareRun(r, value, body.Input)
	if err != nil {
		return cerror(422, "invalid_workflow", "Workflow or pinned dependencies are invalid.")
	}
	tagRun(&prepared, id, "consumer", false)
	// Refresh active reservations before enforcing concurrency. Unknown executions
	// retain their slot; an outage must never accidentally release quota.
	l, _, err = g.ledger(r.Context(), a.ClientID)
	if err != nil {
		return err
	}
	for _, entry := range l.Runs {
		if !consumer.Terminal(entry.Status) {
			if err = g.refreshConsumerRun(r.Context(), s, a, entry); err != nil {
				return err
			}
		}
	}
	var run consumer.Run
	err = g.updateLedger(r.Context(), a.ClientID, func(l *consumer.Ledger) error {
		if existing, ok := l.Runs[id]; ok {
			if existing.Fingerprint != fingerprint {
				return cerror(409, "idempotency_conflict", "This key was already used for a different request.")
			}
			run = existing
			return nil
		}
		active := 0
		for _, v := range l.Runs {
			if !consumer.Terminal(v.Status) {
				active++
			}
		}
		if active >= a.MaxConcurrentRuns {
			return cerror(429, "concurrency_limit", "Concurrent run quota exceeded.")
		}
		run = consumer.Run{ID: id, KeyHash: keyHash, Fingerprint: fingerprint, DefinitionID: body.DefinitionID, Version: body.Version, Sequence: len(l.Runs) + 1, CreatedAt: time.Now().UTC(), Status: "pending"}
		l.Runs[id] = run
		return nil
	})
	if err != nil {
		return err
	}
	// Store immutable payload separately so the admission ledger stays bounded.
	var input model.RunInput
	_, err = g.ConsumerStore.Read(r.Context(), "payload_"+id, &input)
	if errors.Is(err, consumer.ErrNotFound) {
		input = prepared
		_, err = g.ConsumerStore.Swap(r.Context(), "payload_"+id, -1, input)
		if err != nil {
			return err
		}
		_, err = g.ConsumerStore.Read(r.Context(), "payload_"+id, &input)
	}
	if err != nil {
		return err
	}
	if run.TemporalRunID == "" && !consumer.Terminal(run.Status) {
		// Never resubmit an old ambiguous start after Temporal retention may expire.
		if !run.AttemptedAt.IsZero() && time.Since(run.AttemptedAt) > 5*time.Minute {
			return cerror(409, "start_uncertain", "Start outcome is uncertain. Inspect this run; do not use a new key without operator review.")
		}
		namespace := s.namespace
		if namespace == "" {
			namespace = g.TemporalNamespace
		}
		if namespace == "" {
			namespace = "default"
		}
		desc, err := s.temporal.WorkflowService().DescribeNamespace(r.Context(), &workflowservice.DescribeNamespaceRequest{Namespace: namespace})
		if err != nil {
			return err
		}
		if desc.GetConfig().GetWorkflowExecutionRetentionTtl().AsDuration() < time.Hour {
			return cerror(503, "retention_unsafe", "Execution API requires Temporal retention of at least one hour.")
		}
		err = g.updateLedger(r.Context(), a.ClientID, func(l *consumer.Ledger) error {
			current := l.Runs[id]
			if current.AttemptedAt.IsZero() {
				current.AttemptedAt = time.Now().UTC()
			}
			l.Runs[id] = current
			run = current
			return nil
		})
		if err != nil {
			return err
		}
		if run.TemporalRunID == "" && !consumer.Terminal(run.Status) {
			if time.Since(run.AttemptedAt) > 5*time.Minute {
				return cerror(409, "start_uncertain", "Start outcome is uncertain; operator review required.")
			}
			started, startErr := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: id, TaskQueue: s.taskQueue, TypedSearchAttributes: input.Metadata.SearchAttributes(), WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL, WorkflowExecutionErrorWhenAlreadyStarted: true}, model.WorkflowType("RunloomWorkflow", input), input)
			if startErr != nil {
				var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
				if !errors.As(startErr, &duplicate) {
					return startErr
				}
				run.TemporalRunID = duplicate.RunId
			} else {
				run.TemporalRunID = started.GetRunID()
			}
			if run.TemporalRunID == "" {
				return fmt.Errorf("missing Temporal run identity")
			}
			err = g.updateLedger(r.Context(), a.ClientID, func(l *consumer.Ledger) error {
				current := l.Runs[id]
				current.TemporalRunID = run.TemporalRunID
				if current.Status == "pending" {
					current.Status = "running"
				}
				l.Runs[id] = current
				run = current
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	if _, err = s.modules.Runs.GetRun(id); errors.Is(err, store.ErrNotFound) {
		_, err = s.modules.Runs.RecordRunStart(store.RunRecord{Metadata: input.Metadata, WorkflowID: id, TemporalRunID: run.TemporalRunID, DefinitionID: body.DefinitionID, DefinitionVersion: body.Version, TriggerType: "consumer", Input: input.Input, TemporalStatus: run.Status})
		if err != nil {
			if _, readErr := s.modules.Runs.GetRun(id); readErr != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	_, err = s.modules.Audits.AppendAudit(store.AuditEvent{EventID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(id+":consumer-started")).String(), WorkflowID: id, DefinitionID: body.DefinitionID, EventType: "workflow.started", Actor: "service-account:" + a.ClientID, Payload: map[string]any{"client_id": a.ClientID, "definition_version": body.Version, "trigger_type": "consumer"}})
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/runs/"+id)
	writeJSON(w, 202, map[string]any{"id": id, "definition_id": body.DefinitionID, "version": body.Version, "status": run.Status})
	return nil
}

func (g *Gateway) refreshConsumerRun(ctx context.Context, s *Server, a consumer.Account, run consumer.Run) error {
	if consumer.Terminal(run.Status) {
		return nil
	}
	d, err := s.temporal.DescribeWorkflowExecution(ctx, run.ID, run.TemporalRunID)
	if err != nil {
		var missing *serviceerror.NotFound
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	status := strings.ToLower(d.GetWorkflowExecutionInfo().GetStatus().String())
	temporalID := d.GetWorkflowExecutionInfo().GetExecution().GetRunId()
	return g.updateLedger(ctx, a.ClientID, func(l *consumer.Ledger) error {
		current, ok := l.Runs[run.ID]
		if !ok {
			return consumer.ErrNotFound
		}
		if !consumer.Terminal(current.Status) {
			current.Status = status
			current.TemporalRunID = temporalID
			l.Runs[run.ID] = current
		}
		return nil
	})
}
func (g *Gateway) consumerRuns(w http.ResponseWriter, r *http.Request, s *Server, a consumer.Account) error {
	limit, after, err := pageOptions(r)
	if err != nil {
		return err
	}
	l, _, err := g.ledger(r.Context(), a.ClientID)
	if err != nil {
		return err
	}
	entries := []consumer.Run{}
	for _, run := range l.Runs {
		if run.Sequence <= after {
			continue
		}
		if _, ok := a.Grant(run.DefinitionID, run.Version); !ok {
			continue
		}
		if id := r.URL.Query().Get("definition_id"); id != "" && run.DefinitionID != id {
			continue
		}
		entries = append(entries, run)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	next := ""
	if len(entries) > limit {
		next = strconv.Itoa(entries[limit-1].Sequence)
		entries = entries[:limit]
	}
	items := []map[string]any{}
	for _, run := range entries {
		items = append(items, map[string]any{"id": run.ID, "definition_id": run.DefinitionID, "version": run.Version, "status": run.Status, "created_at": run.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
	return nil
}
func (g *Gateway) consumerRun(w http.ResponseWriter, r *http.Request, s *Server, a consumer.Account, path string, cancel bool) error {
	id := strings.TrimPrefix(path, "runs/")
	if cancel {
		id = strings.TrimSuffix(id, "/cancel")
	}
	if strings.Contains(id, "/") {
		return cerror(404, "not_found", "Run not found.")
	}
	l, _, err := g.ledger(r.Context(), a.ClientID)
	if err != nil {
		return err
	}
	run, ok := l.Runs[id]
	if !ok {
		return cerror(404, "not_found", "Run not found.")
	}
	grant, ok := a.Grant(run.DefinitionID, run.Version)
	if !ok {
		return cerror(404, "not_found", "Run not found.")
	}
	if err = g.refreshConsumerRun(r.Context(), s, a, run); err != nil {
		return err
	}
	l, _, err = g.ledger(r.Context(), a.ClientID)
	if err != nil {
		return err
	}
	run = l.Runs[id]
	if cancel {
		if run.TemporalRunID == "" {
			return cerror(409, "start_pending", "Run has not been confirmed; retry status before canceling.")
		}
		_, err = s.modules.Audits.AppendAudit(store.AuditEvent{WorkflowID: id, DefinitionID: run.DefinitionID, EventType: "workflow.cancel_requested", Actor: "service-account:" + a.ClientID, Payload: map[string]any{"client_id": a.ClientID}})
		if err != nil {
			return err
		}
		if !consumer.Terminal(run.Status) {
			if err = s.temporal.CancelWorkflow(r.Context(), id, run.TemporalRunID); err != nil {
				return err
			}
		}
		writeJSON(w, 202, map[string]any{"id": id, "cancel_requested": true})
		return nil
	}
	response := map[string]any{"id": id, "definition_id": run.DefinitionID, "version": run.Version, "status": run.Status, "created_at": run.CreatedAt}
	// Budget pauses remain nonterminal in Temporal. Expose only a safe reason,
	// never policy names, project spend, node data or another consumer's usage.
	if run.TemporalRunID != "" && !consumer.Terminal(run.Status) {
		encoded, queryErr := s.temporal.QueryWorkflow(r.Context(), id, run.TemporalRunID, "status")
		var state model.RunState
		if queryErr == nil && encoded.Get(&state) == nil && state.Status == "paused" && state.PausedStep != nil && state.PausedStep.Budget != nil {
			response["status"] = "paused"
			response["pause_reason"] = "llm_budget_limit"
			if state.PausedStep.Budget.Reason == "budget_accounting_unavailable" {
				response["pause_reason"] = "llm_budget_accounting_unavailable"
			}
		}
	}
	if len(grant.InputFields) > 0 {
		var input model.RunInput
		if _, err = g.ConsumerStore.Read(r.Context(), "payload_"+id, &input); err != nil {
			return err
		}
		response["input"] = exposeFields(input.Input, grant.InputFields)
	}
	// Never return raw state, node data, errors, child IDs or arbitrary outputs.
	if len(grant.OutputFields) > 0 && run.TemporalRunID != "" {
		encoded, queryErr := s.temporal.QueryWorkflow(r.Context(), id, run.TemporalRunID, "status")
		var state model.RunState
		if queryErr == nil && encoded.Get(&state) == nil {
			response["output"] = exposeFields(state.Output, grant.OutputFields)
		} else if saved, readErr := s.modules.Runs.GetRun(id); readErr == nil {
			response["output"] = exposeFields(saved.Output, grant.OutputFields)
		}
	}
	writeJSON(w, 200, response)
	return nil
}
func exposeFields(v any, fields []string) map[string]any {
	result := map[string]any{}
	object, ok := v.(map[string]any)
	if !ok {
		return result
	}
	for _, key := range fields {
		if val, exists := object[key]; exists {
			result[key] = redact(key, val)
		}
	}
	return result
}
func redact(key string, v any) any {
	lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	for _, s := range []string{"password", "secret", "token", "authorization", "apikey", "cookie"} {
		if strings.Contains(lower, s) {
			return "[REDACTED]"
		}
	}
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			out[k] = redact(k, val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redact("", val)
		}
		return out
	}
	return v
}
