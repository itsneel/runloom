// Package budget defines reusable, metadata-only monthly UTC budget policies.
package budget

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

const PauseError = "LLMBudgetPaused"

type Policy struct {
	ID       string `json:"id" bson:"id"`
	Revision int64  `json:"revision" bson:"revision"`
	Name     string `json:"name" bson:"name"`
	Enabled  bool   `json:"enabled" bson:"enabled"`
	SoftUSD  string `json:"soft_usd" bson:"soft_usd"`
	HardUSD  string `json:"hard_usd" bson:"hard_usd"`
	// Empty means all current and future projects in the owning team.
	Projects          []string `json:"projects" bson:"projects"`
	ExcludedWorkflows []string `json:"excluded_workflows" bson:"excluded_workflows"`
	Reason            string   `json:"reason" bson:"reason"`
	UpdatedAt         string   `json:"updated_at" bson:"updated_at"`
}

type Change struct {
	Actor  string `json:"actor" bson:"actor"`
	Policy Policy `json:"policy" bson:"policy"`
}

type Decision struct {
	PolicyID       string `json:"policy_id,omitempty"`
	PolicyName     string `json:"policy_name,omitempty"`
	PolicyRevision int64  `json:"policy_revision,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	ProjectName    string `json:"project_name,omitempty"`
	Month          string `json:"month,omitempty"`
	ResetsAt       string `json:"resets_at,omitempty"`
	SpendUSD       string `json:"spend_usd,omitempty"`
	LimitUSD       string `json:"limit_usd,omitempty"`
	Incomplete     bool   `json:"incomplete"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

var amount = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,12})?$`)

func Validate(p Policy) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || strings.ContainsFunc(p.Name, unicode.IsControl) || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 500 {
		return fmt.Errorf("a policy name (1–100 characters) and change reason (1–500 characters) are required")
	}
	if p.SoftUSD == "" && p.HardUSD == "" {
		return fmt.Errorf("set a soft limit, hard limit, or both")
	}
	for _, value := range []string{p.SoftUSD, p.HardUSD} {
		if value != "" && !amount.MatchString(value) {
			return fmt.Errorf("limits must be nonnegative decimal USD amounts with at most 12 decimal places")
		}
	}
	if p.SoftUSD != "" && p.HardUSD != "" && Money(p.SoftUSD).Cmp(Money(p.HardUSD)) > 0 {
		return fmt.Errorf("soft limit must not exceed hard limit")
	}
	if len(p.Projects) > 100 || len(p.ExcludedWorkflows) > 500 {
		return fmt.Errorf("policy supports at most 100 selected projects and 500 exclusions")
	}
	return nil
}

func Money(s string) *big.Rat {
	n, ok := new(big.Rat).SetString(s)
	if !ok {
		return new(big.Rat)
	}
	return n
}
func (p Policy) Applies(project, definition string) bool {
	return p.Enabled && (len(p.Projects) == 0 || slices.Contains(p.Projects, project)) && !slices.Contains(p.ExcludedWorkflows, definition)
}
func Month(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}
func Unavailable(project string) Decision {
	return Decision{ProjectID: project, Reason: "budget_accounting_unavailable", Message: "LLM budget accounting is unavailable. Further LLM calls are blocked and completed results are retained. Retry Resume after accounting is restored."}
}

func Evaluate(p Policy, project, name string, now time.Time, spend string, incomplete bool) []Decision {
	start, end := Month(now)
	decisions := []Decision{}
	for _, limit := range []struct{ value, reason string }{{p.SoftUSD, "soft_limit_reached"}, {p.HardUSD, "hard_limit_reached"}} {
		if limit.value == "" || Money(spend).Cmp(Money(limit.value)) < 0 {
			continue
		}
		message := fmt.Sprintf("%s: project %s has recorded $%s against its $%s monthly UTC limit (%s).", p.Name, name, spend, limit.value, start.Format("2006-01"))
		if limit.reason == "hard_limit_reached" {
			message += " This hard limit pauses affected workflows. Resume rechecks the budget; it does not override the limit."
		}
		decisions = append(decisions, Decision{PolicyID: p.ID, PolicyName: p.Name, PolicyRevision: p.Revision, ProjectID: project, ProjectName: name, Month: start.Format("2006-01"), ResetsAt: end.Format(time.RFC3339), SpendUSD: spend, LimitUSD: limit.value, Incomplete: incomplete, Reason: limit.reason, Message: message})
	}
	return decisions
}
