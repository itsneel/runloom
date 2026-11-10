// Package usage contains metadata only: never prompts, responses or credentials.
package usage

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	ProjectID         string   `json:"project_id,omitempty"`
	ID                string   `json:"id"`
	TeamID            string   `json:"team_id"`
	WorkflowID        string   `json:"workflow_id"`
	TemporalRunID     string   `json:"temporal_run_id"`
	Ancestors         []string `json:"ancestors"`
	DefinitionID      string   `json:"definition_id"`
	DefinitionVersion int      `json:"definition_version"`
	StepID            string   `json:"step_id"`
	ActivityID        string   `json:"activity_id"`
	Attempt           int32    `json:"attempt"`
	IsTest            bool     `json:"is_test"`
	Provider          string   `json:"provider"`
	RequestedModel    string   `json:"requested_model"`
	Model             string   `json:"model"`
	ProviderRequestID string   `json:"provider_request_id,omitempty"`
	StartedAt         string   `json:"started_at"`
	FinishedAt        string   `json:"finished_at,omitempty"`
	DurationMS        int64    `json:"duration_ms"`
	Status            string   `json:"status"`     // pending, succeeded, failed
	Accounting        string   `json:"accounting"` // pending, unknown, unpriced, estimated
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	CachedTokens      int64    `json:"cached_tokens"`
	ReasoningTokens   int64    `json:"reasoning_tokens"` // subset of output, not added again
	CostUSD           string   `json:"cost_usd,omitempty"`
	Pricing           *Rate    `json:"pricing,omitempty"`
}

func RunKey(id, run string) string { return id + ":" + run }

type Filter struct {
	RunKey, Model, StepID, After string
	Limit                        int
	// All is only used after authorizing a team-scoped analytics query. Bounds
	// are UTC, second-precision ISO prefixes without Z (inclusive/exclusive).
	All                        bool
	StartedFrom, StartedBefore string
}

func (f Filter) Match(e Entry) bool {
	found := f.All
	for _, key := range e.Ancestors {
		if key == f.RunKey {
			found = true
			break
		}
	}
	return found && (f.Model == "" || e.Model == f.Model) && (f.StepID == "" || e.StepID == f.StepID) && e.ID > f.After && (f.StartedFrom == "" || e.StartedAt >= f.StartedFrom) && (f.StartedBefore == "" || e.StartedAt < f.StartedBefore)
}

type Rate struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Version     string `json:"version"`
	EffectiveAt string `json:"effective_at"`
	Currency    string `json:"currency"`
	Input       string `json:"input_per_million"`
	Output      string `json:"output_per_million"`
	Cached      string `json:"cached_input_per_million,omitempty"`
}
type Catalogue struct {
	Rates []Rate `json:"rates"`
}

var ratePattern = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,6})?$`)

func Load(path string) (Catalogue, error) {
	if path == "" {
		return Catalogue{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return Catalogue{}, fmt.Errorf("cannot read LLM pricing catalogue")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return Catalogue{}, fmt.Errorf("cannot read LLM pricing catalogue")
	}
	if len(raw) > 1024*1024 {
		return Catalogue{}, fmt.Errorf("LLM pricing catalogue exceeds 1 MiB")
	}
	var c Catalogue
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid LLM pricing catalogue")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("invalid trailing data in LLM pricing catalogue")
	}
	return c, c.Validate()
}
func (c Catalogue) Validate() error {
	seen := map[string]bool{}
	for _, r := range c.Rates {
		at, err := time.Parse(time.RFC3339, r.EffectiveAt)
		key := r.Provider + "\x00" + r.Model + "\x00" + at.UTC().Format(time.RFC3339Nano)
		if err != nil || r.Provider == "" || len(r.Provider) > 100 || r.Model == "" || len(r.Model) > 200 || r.Version == "" || len(r.Version) > 100 || r.Currency != "USD" || !ratePattern.MatchString(r.Input) || !ratePattern.MatchString(r.Output) || (r.Cached != "" && !ratePattern.MatchString(r.Cached)) || seen[key] {
			return fmt.Errorf("invalid or duplicate LLM pricing rate; use exact models, USD, version, effective_at and nonnegative decimal rates")
		}
		seen[key] = true
	}
	return nil
}

func token(m map[string]any, key string) (int64, bool) {
	value, ok := m[key]
	if !ok {
		return 0, false
	}
	var n int64
	switch v := value.(type) {
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		n = parsed
	case float64:
		n = int64(v)
		if float64(n) != v {
			return 0, false
		}
	case int64:
		n = v
	case int:
		n = int64(v)
	default:
		return 0, false
	}
	return n, n >= 0 && n <= 1_000_000_000_000
}

// OpenAI-compatible chat usage adapter. Optional nested counts are subsets.
// Unknown shapes stay unknown instead of inventing a split from total_tokens.
func (c Catalogue) Price(e *Entry, raw map[string]any) {
	e.Accounting = "unknown"
	input, ok := token(raw, "prompt_tokens")
	if !ok {
		return
	}
	output, ok := token(raw, "completion_tokens")
	if !ok {
		return
	}
	cached, reasoning := int64(0), int64(0)
	if details, exists := raw["prompt_tokens_details"]; exists && details != nil {
		m, valid := details.(map[string]any)
		if !valid {
			return
		}
		if _, exists := m["cached_tokens"]; exists {
			cached, ok = token(m, "cached_tokens")
			if !ok || cached > input {
				return
			}
		}
	}
	if details, exists := raw["completion_tokens_details"]; exists && details != nil {
		m, valid := details.(map[string]any)
		if !valid {
			return
		}
		if _, exists := m["reasoning_tokens"]; exists {
			reasoning, ok = token(m, "reasoning_tokens")
			if !ok || reasoning > output {
				return
			}
		}
	}
	e.InputTokens = input
	e.OutputTokens = output
	e.CachedTokens = cached
	e.ReasoningTokens = reasoning
	e.Accounting = "unpriced"
	// Unsupported billable modalities need a provider-specific adapter.
	for _, key := range []string{"cache_creation_input_tokens", "audio_tokens", "image_tokens"} {
		if value, present := raw[key]; present && fmt.Sprint(value) != "0" {
			return
		}
	}
	for _, key := range []string{"prompt_tokens_details", "completion_tokens_details"} {
		if details, ok := raw[key].(map[string]any); ok {
			for _, extra := range []string{"audio_tokens", "image_tokens"} {
				if value, present := details[extra]; present && fmt.Sprint(value) != "0" {
					return
				}
			}
		}
	}
	started, err := time.Parse(time.RFC3339Nano, e.StartedAt)
	if err != nil {
		return
	}
	var selected *Rate
	var latest time.Time
	for _, rate := range c.Rates {
		at, _ := time.Parse(time.RFC3339, rate.EffectiveAt)
		if rate.Provider == e.Provider && rate.Model == e.Model && !at.After(started) && (selected == nil || at.After(latest)) {
			copy := rate
			selected = &copy
			latest = at
		}
	}
	if selected == nil || (cached > 0 && selected.Cached == "") {
		return
	}
	cost := new(big.Rat)
	for _, part := range []struct {
		count int64
		rate  string
	}{{input - cached, selected.Input}, {output, selected.Output}, {cached, selected.Cached}} {
		if part.count == 0 {
			continue
		}
		rate, ok := new(big.Rat).SetString(part.rate)
		if !ok {
			return
		}
		cost.Add(cost, new(big.Rat).Mul(rate, big.NewRat(part.count, 1_000_000)))
	}
	e.CostUSD = cost.FloatString(12)
	e.Pricing = selected
	e.Accounting = "estimated"
}

type Summary struct {
	Nodes        []NodeSummary `json:"nodes"`
	Models       []string      `json:"models"`
	EstimatedUSD string        `json:"estimated_usd"`
	DirectUSD    string        `json:"direct_usd"`
	ChildUSD     string        `json:"child_usd"`
	Requests     int           `json:"requests"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	Unknown      int           `json:"unknown"`
	Unpriced     int           `json:"unpriced"`
	Pending      int           `json:"pending"`
	Complete     bool          `json:"complete"`
	Truncated    bool          `json:"truncated"`
}

type NodeSummary struct {
	WorkflowID   string `json:"workflow_id"`
	StepID       string `json:"step_id"`
	Requests     int    `json:"requests"`
	Incomplete   int    `json:"incomplete"`
	EstimatedUSD string `json:"estimated_usd"`
}

func Summarize(entries []Entry, root string, truncated bool) Summary {
	s := Summary{Complete: !truncated, Truncated: truncated}
	nodes := map[string]*NodeSummary{}
	costs := map[string]*big.Rat{}
	models := map[string]bool{}
	direct, children := new(big.Rat), new(big.Rat)
	for _, e := range entries {
		key := RunKey(e.WorkflowID, e.TemporalRunID) + ":" + e.StepID
		if nodes[key] == nil {
			nodes[key] = &NodeSummary{WorkflowID: e.WorkflowID, StepID: e.StepID}
			costs[key] = new(big.Rat)
		}
		nodes[key].Requests++
		models[e.Model] = true
		if e.Accounting != "estimated" {
			nodes[key].Incomplete++
		} else if cost, ok := new(big.Rat).SetString(e.CostUSD); ok {
			costs[key].Add(costs[key], cost)
		} else {
			nodes[key].Incomplete++
		}
		s.Requests++
		s.InputTokens += e.InputTokens
		s.OutputTokens += e.OutputTokens
		switch e.Accounting {
		case "pending":
			s.Pending++
			s.Complete = false
		case "unknown":
			s.Unknown++
			s.Complete = false
		case "unpriced":
			s.Unpriced++
			s.Complete = false
		case "estimated":
			cost, ok := new(big.Rat).SetString(e.CostUSD)
			if !ok {
				s.Unknown++
				s.Complete = false
				continue
			}
			if RunKey(e.WorkflowID, e.TemporalRunID) == root {
				direct.Add(direct, cost)
			} else {
				children.Add(children, cost)
			}
		default:
			s.Unknown++
			s.Complete = false
		}
	}
	s.DirectUSD = direct.FloatString(12)
	keys := []string{}
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	s.Nodes = []NodeSummary{}
	for _, key := range keys {
		nodes[key].EstimatedUSD = costs[key].FloatString(12)
		s.Nodes = append(s.Nodes, *nodes[key])
	}
	s.Models = []string{}
	for model := range models {
		s.Models = append(s.Models, model)
	}
	sort.Strings(s.Models)
	s.ChildUSD = children.FloatString(12)
	s.EstimatedUSD = new(big.Rat).Add(direct, children).FloatString(12)
	return s
}
