package services

import (
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

type Internal struct {
	Role      string
	Trust     Trust
	Directory *access.Directory
	Verifier  access.Verifier
	OpenTeam  func(*http.Request, string) (store.Backend, func(), error)
}

func (h *Internal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller := h.Trust.caller(r)
	if caller == "" {
		http.Error(w, "service authentication required", 401)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/")
	if h.Role == "auth" && (caller == "workflows" || caller == "management") {
		switch path {
		case "identity/bind":
			var b struct{ Token string }
			if !decode(w, r, &b) {
				return
			}
			user, err := h.Verifier.Verify(r.Context(), b.Token)
			if err != nil {
				reply(w, nil, access.ErrForbidden)
				return
			}
			state, err := h.Directory.Bind(r.Context(), user)
			state.Secrets = nil
			if caller == "management" {
				state.ServiceAccounts = nil
			}
			reply(w, state, err)
			return
		case "identity/view":
			var b struct{ Scope string }
			if !decode(w, r, &b) {
				return
			}
			if b.Scope != "identity" && (b.Scope != "consumers" || caller != "workflows") {
				reply(w, nil, access.ErrForbidden)
				return
			}
			state, _, err := h.Directory.Repo.Module(b.Scope).Read(r.Context())
			state.Secrets = nil
			if caller == "management" {
				state.ServiceAccounts = nil
			}
			reply(w, state, err)
			return
		}
	}
	parts := strings.Split(path, "/")
	if h.Role == "workflows" && len(parts) == 4 && parts[0] == "orgs" && parts[2] == "secrets" && parts[3] == "audit" {
		if caller != "auth" {
			reply(w, nil, access.ErrForbidden)
			return
		}
		org, err := uuid.Parse(parts[1])
		if err != nil || org.String() != parts[1] {
			http.Error(w, "invalid organization", 400)
			return
		}
		var input struct{}
		if !decode(w, r, &input) {
			return
		}
		state, _, err := h.Directory.Repo.Module("secrets").ReadOwned(r.Context())
		events := []access.Event{}
		for _, event := range state.Events {
			if event.OrgID == parts[1] {
				events = append(events, event)
			}
		}
		reply(w, events, err)
		return
	}
	if len(parts) != 4 || parts[0] != "teams" {
		http.NotFound(w, r)
		return
	}
	team, err := uuid.Parse(parts[1])
	if err != nil || team.String() != parts[1] {
		http.Error(w, "invalid team", 400)
		return
	}
	op := parts[2] + "/" + parts[3]
	// Operation permissions are independent of routing and database credentials.
	allowed := false
	if h.Role == "workflows" {
		allowed = (caller == "auth" && op == "definition/get") || (caller == "management" && (op == "definition/get" || op == "definition/list" || op == "projects/get" || op == "audit/list" || op == "run/get"))
	}
	if h.Role == "management" {
		allowed = (caller == "workflows" || caller == "worker") && (strings.HasPrefix(op, "prompts/") || op == "policies/get" || strings.HasPrefix(op, "usage/"))
		if op == "usage/begin" || op == "usage/finish" {
			allowed = caller == "worker"
		}
	}
	if !allowed {
		reply(w, nil, access.ErrForbidden)
		return
	}
	b, release, err := h.OpenTeam(r, parts[1])
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer release()
	ctx := r.Context()
	switch op {
	case "definition/get":
		var p struct {
			ID      string
			Version *int
		}
		if !decode(w, r, &p) {
			return
		}
		v, e := b.Get(p.ID, p.Version)
		reply(w, v, e)
	case "definition/list":
		var p struct{}
		if !decode(w, r, &p) {
			return
		}
		v, e := b.List()
		reply(w, v, e)
	case "projects/get", "policies/get":
		var p struct{}
		if !decode(w, r, &p) {
			return
		}
		v, e := b.ProjectCatalog(ctx)
		if op == "projects/get" {
			v.BudgetPolicies = nil
			v.BudgetChanges = nil
			v.EditLocks = nil
		} else {
			v.Projects = nil
			v.WorkflowProjects = nil
			v.EditLocks = nil
		}
		reply(w, v, e)
	case "run/get":
		var p struct{ ID string }
		if !decode(w, r, &p) {
			return
		}
		v, e := b.GetRun(p.ID)
		reply(w, v, e)
	case "audit/list":
		var p struct {
			Run, Definition string
			Limit           int
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Limit < 1 || p.Limit > 1000 {
			http.Error(w, "invalid limit", 400)
			return
		}
		v, e := b.ListAudit(p.Run, p.Definition, p.Limit)
		reply(w, v, e)
	case "prompts/get":
		var p struct {
			ID      string
			Version int
		}
		if !decode(w, r, &p) {
			return
		}
		v, e := b.GetPrompt(ctx, p.ID, p.Version)
		reply(w, v, e)
	case "prompts/list":
		var p struct{}
		if !decode(w, r, &p) {
			return
		}
		v, e := b.ListPrompts(ctx)
		reply(w, v, e)
	case "prompts/history":
		var p struct{ ID string }
		if !decode(w, r, &p) {
			return
		}
		v, e := b.PromptHistory(ctx, p.ID)
		reply(w, v, e)
	case "usage/begin":
		var p usage.Entry
		if !decode(w, r, &p) {
			return
		}
		v, e := b.BeginUsage(ctx, p)
		reply(w, v, e)
	case "usage/finish":
		var p usage.Entry
		if !decode(w, r, &p) {
			return
		}
		e := b.FinishUsage(ctx, p)
		reply(w, e == nil, e)
	case "usage/query":
		var p usage.Filter
		if !decode(w, r, &p) {
			return
		}
		v, e := b.QueryUsage(ctx, p)
		reply(w, v, e)
	case "usage/aggregate":
		var p struct{ Start, End string }
		if !decode(w, r, &p) {
			return
		}
		v, e := b.AggregateSpend(ctx, p.Start, p.End)
		reply(w, v, e)
	default:
		http.NotFound(w, r)
	}
}
