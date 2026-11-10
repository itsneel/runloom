package services

import (
	"github.com/example/runloom/management/internal/access"
	"github.com/example/runloom/management/internal/store"
	"github.com/example/runloom/management/internal/usage"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

type Internal struct {
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
	parts := strings.Split(path, "/")
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

	{
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
	case "policies/get":
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
