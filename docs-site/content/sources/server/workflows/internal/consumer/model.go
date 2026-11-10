package consumer

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

var Scopes = []string{"workflows:read", "runs:start", "runs:read", "runs:cancel"}

type Grant struct {
	DefinitionID string   `json:"definition_id"`
	Versions     []int    `json:"versions"`
	InputFields  []string `json:"input_fields"`
	OutputFields []string `json:"output_fields"`
}
type Policy struct {
	Name              string   `json:"name"`
	Scopes            []string `json:"scopes"`
	Grants            []Grant  `json:"grants"`
	RequestsPerMinute int      `json:"requests_per_minute"`
	MaxConcurrentRuns int      `json:"max_concurrent_runs"`
}
type Account struct {
	Policy
	ClientID    string    `json:"client_id"`
	TeamID      string    `json:"team_id"`
	OrgID       string    `json:"org_id"`
	Subject     string    `json:"subject"`
	ProviderID  string    `json:"provider_id"`
	Generation  string    `json:"generation"`
	Status      string    `json:"status"`
	OperationAt time.Time `json:"operation_at"`
	CreatedAt   string    `json:"created_at"`
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 {
		return fmt.Errorf("name must contain 1 to 120 characters")
	}
	if len(p.Scopes) == 0 || len(p.Scopes) > 4 {
		return fmt.Errorf("select 1 to 4 execution scopes")
	}
	for _, s := range p.Scopes {
		if !slices.Contains(Scopes, s) {
			return fmt.Errorf("invalid execution scope")
		}
	}
	if p.RequestsPerMinute < 1 || p.RequestsPerMinute > 10000 || p.MaxConcurrentRuns < 1 || p.MaxConcurrentRuns > 100 {
		return fmt.Errorf("requests_per_minute must be 1..10000 and max_concurrent_runs 1..100")
	}
	if len(p.Grants) == 0 || len(p.Grants) > 100 {
		return fmt.Errorf("select 1 to 100 workflow grants")
	}
	seen := map[string]bool{}
	for _, g := range p.Grants {
		if g.DefinitionID == "" || seen[g.DefinitionID] || len(g.Versions) == 0 || len(g.Versions) > 100 {
			return fmt.Errorf("each workflow must have one explicit grant with 1 to 100 versions")
		}
		seen[g.DefinitionID] = true
		for _, v := range g.Versions {
			if v < 1 {
				return fmt.Errorf("only explicit published versions are allowed")
			}
		}
		for _, fields := range [][]string{g.InputFields, g.OutputFields} {
			if len(fields) > 100 {
				return fmt.Errorf("too many exposed fields")
			}
			for _, f := range fields {
				if f == "" || f == "*" || len(f) > 120 {
					return fmt.Errorf("exposure requires named top-level fields; wildcard is forbidden")
				}
			}
		}
	}
	return nil
}
func (a Account) Grant(id string, version int) (Grant, bool) {
	for _, g := range a.Grants {
		if g.DefinitionID == id && slices.Contains(g.Versions, version) {
			return g, true
		}
	}
	return Grant{}, false
}

type Identity struct {
	ClientID   string
	Subject    string
	Generation string
	Scopes     []string
}
type Run struct {
	ID            string    `json:"id"`
	KeyHash       string    `json:"key_hash"`
	Fingerprint   string    `json:"fingerprint"`
	DefinitionID  string    `json:"definition_id"`
	Version       int       `json:"version"`
	Sequence      int       `json:"sequence"`
	CreatedAt     time.Time `json:"created_at"`
	AttemptedAt   time.Time `json:"attempted_at"`
	TemporalRunID string    `json:"temporal_run_id"`
	Status        string    `json:"status"`
}
type Ledger struct {
	Runs     map[string]Run `json:"runs"`
	Minute   int64          `json:"minute"`
	Requests int            `json:"requests"`
}

func NewLedger() Ledger { return Ledger{Runs: map[string]Run{}} }
func Terminal(s string) bool {
	return slices.Contains([]string{"completed", "failed", "canceled", "terminated", "timedout", "continuedasnew"}, s)
}
