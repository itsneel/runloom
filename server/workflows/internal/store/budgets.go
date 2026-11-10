package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/budget"
	"github.com/google/uuid"
)

var ErrBudgetConflict = errors.New("policy changed; refresh before saving")
var ErrBudgetInput = errors.New("invalid budget policy")

func invalidBudget(message string) error { return fmt.Errorf("%w: %s", ErrBudgetInput, message) }

func (s *projectStore) SaveBudgetPolicy(ctx context.Context, p budget.Policy, actor string) (budget.Policy, error) {
	if err := budget.Validate(p); err != nil {
		return p, invalidBudget(err.Error())
	}
	if _, err := uuid.Parse(p.ID); err != nil {
		return p, invalidBudget("invalid policy ID")
	}
	for _, id := range p.ExcludedWorkflows {
		if !projectDefinitionID.MatchString(id) {
			return p, invalidBudget("invalid excluded workflow ID")
		}
		v := 0
		if _, err := s.getDefinition(id, &v); err != nil {
			if _, err = s.getDefinition(id, nil); err != nil {
				return p, invalidBudget("excluded workflow not found in this team")
			}
		}
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	projects, _, err := s.read(ctx, "projects")
	if s.policyProjects != nil {
		projects, err = s.policyProjects(ctx)
	}
	if err != nil {
		return p, err
	}
	err = s.updateScope(ctx, "policies", func(c *ProjectCatalog) error {
		previous, exists := c.BudgetPolicies[p.ID]
		if p.Revision != previous.Revision || (!exists && p.Revision != 0) {
			return ErrBudgetConflict
		}
		if !exists && len(c.BudgetPolicies) >= 100 {
			return invalidBudget("at most 100 policies per team")
		}
		for _, id := range p.Projects {
			if _, ok := projects.Projects[id]; !ok {
				return ErrProjectNotFound
			}
		}
		if c.BudgetPolicies == nil {
			c.BudgetPolicies = map[string]budget.Policy{}
		}
		next := p
		next.Name = strings.TrimSpace(next.Name)
		next.Revision++
		c.BudgetPolicies[p.ID] = next
		// Policy changes and their actor/reason commit atomically with project metadata.
		c.BudgetChanges = append(c.BudgetChanges, budget.Change{Actor: actor, Policy: next})
		return nil
	})
	if err != nil {
		return p, err
	}
	p.Revision++
	return p, nil
}

type BudgetSpend struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	SpendUSD    string `json:"spend_usd"`
	Incomplete  bool   `json:"incomplete"`
	Requests    int    `json:"requests"`
}

// MonthlyBudgetSpend reads direct requests exactly once, never descendant totals.
// Fail rather than returning a partial total if storage or scan bounds fail.
func MonthlyBudgetSpend(ctx context.Context, b Metering, c ProjectCatalog, now time.Time) (map[string]BudgetSpend, error) {
	start, end := budget.Month(now)
	totals := map[string]BudgetSpend{}
	for id, p := range c.Projects {
		totals[id] = BudgetSpend{ProjectID: id, ProjectName: p.Name, SpendUSD: "0.000000000000"}
	}
	rows, err := b.AggregateSpend(ctx, start.Format("2006-01-02T15:04:05"), end.Format("2006-01-02T15:04:05"))
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		project := e.ProjectID
		if project == "" {
			project = c.WorkflowProject(e.DefinitionID)
		} // pre-attribution ledger records
		total := totals[project]
		total.ProjectID = project
		total.Requests += e.Requests
		total.SpendUSD = budget.Money(total.SpendUSD).Add(budget.Money(total.SpendUSD), budget.Money(e.CostUSD)).FloatString(12)
		total.Incomplete = total.Incomplete || e.Incomplete
		totals[project] = total
	}
	return totals, ctx.Err()
}

type BudgetReader interface {
	Metering
	ProjectCatalog(context.Context) (ProjectCatalog, error)
}

func CheckLLMBudget(ctx context.Context, b BudgetReader, definition string, now time.Time) (string, []budget.Decision, error) {
	c, err := b.ProjectCatalog(ctx)
	if err != nil {
		return "", nil, err
	}
	project := c.WorkflowProject(definition)
	policies := []budget.Policy{}
	for _, p := range c.BudgetPolicies {
		if p.Applies(project, definition) {
			policies = append(policies, p)
		}
	}
	if len(policies) == 0 {
		return project, nil, nil
	}
	totals, err := MonthlyBudgetSpend(ctx, b, c, now)
	if err != nil {
		return project, nil, err
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
	decisions := []budget.Decision{}
	for _, p := range policies {
		t := totals[project]
		decisions = append(decisions, budget.Evaluate(p, project, c.Projects[project].Name, now, t.SpendUSD, t.Incomplete)...)
	}
	return project, decisions, nil
}
