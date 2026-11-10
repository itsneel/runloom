package usage

import (
	"fmt"
	"math/big"
	"sort"
	"time"
)

// Window ends on a UTC second boundary. Requests belong to the period in which
// they started, even if their accounting is finalized later.
type Window struct {
	Period       string    `json:"period"`
	From         time.Time `json:"from"`
	Until        time.Time `json:"until"`
	PreviousFrom time.Time `json:"previous_from"`
	Bucket       string    `json:"bucket"`
}

func AnalyticsWindow(period string, now time.Time) (Window, error) {
	end := now.UTC().Truncate(time.Second)
	w := Window{Period: period, Until: end, Bucket: "day"}
	switch period {
	case "1d":
		w.From = end.Add(-24 * time.Hour)
		w.Bucket = "hour"
	case "1w":
		w.From = end.Add(-7 * 24 * time.Hour)
	case "1m":
		w.From = monthsBefore(end, 1)
	case "mtd":
		w.From = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "ytd":
		w.From = time.Date(end.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		w.Bucket = "month"
	case "6m":
		w.From = monthsBefore(end, 6)
		w.Bucket = "month"
	case "1y":
		w.From = monthsBefore(end, 12)
		w.Bucket = "month"
	default:
		return w, fmt.Errorf("period must be 1d, 1w, 1m, mtd, ytd, 6m or 1y")
	}
	w.PreviousFrom = w.From.Add(-end.Sub(w.From))
	return w, nil
}
func monthsBefore(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month(), 1, t.Hour(), t.Minute(), t.Second(), 0, time.UTC).AddDate(0, -n, 0)
	last := first.AddDate(0, 1, -1).Day()
	day := t.Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}

type Metrics struct {
	EstimatedUSD   string  `json:"estimated_usd"`
	AverageUSD     *string `json:"average_usd"`
	Requests       int     `json:"requests"`
	Priced         int     `json:"priced"`
	Unknown        int     `json:"unknown"`
	Unpriced       int     `json:"unpriced"`
	Pending        int     `json:"pending"`
	Runs           int     `json:"runs"`
	PricedRuns     int     `json:"priced_runs"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	FailedRequests int     `json:"failed_requests"`
	RetryRequests  int     `json:"retry_requests"`
}
type AnalyticsRow struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	TeamID            string `json:"team_id,omitempty"`
	TeamName          string `json:"team_name,omitempty"`
	WorkflowID        string `json:"workflow_id,omitempty"`
	TemporalRunID     string `json:"temporal_run_id,omitempty"`
	DefinitionID      string `json:"definition_id,omitempty"`
	DefinitionVersion int    `json:"definition_version,omitempty"`
	LastRequestAt     string `json:"last_request_at,omitempty"`
	Metrics
}
type Analytics struct {
	Window   Window         `json:"window"`
	Summary  Metrics        `json:"summary"`
	Previous Metrics        `json:"previous"`
	Trend    []AnalyticsRow `json:"trend"`
	Teams    []AnalyticsRow `json:"teams"`
	Runs     []AnalyticsRow `json:"runs"`
	Models   []AnalyticsRow `json:"models"`
}
type accumulator struct {
	row              AnalyticsRow
	cost             *big.Rat
	runs, pricedRuns map[string]bool
}

func accumulatorFor(rows map[string]*accumulator, id, name string) *accumulator {
	if rows[id] == nil {
		rows[id] = &accumulator{row: AnalyticsRow{ID: id, Name: name}, cost: new(big.Rat), runs: map[string]bool{}, pricedRuns: map[string]bool{}}
	}
	return rows[id]
}
func (a *accumulator) add(e Entry, now time.Time) {
	m := &a.row.Metrics
	m.Requests++
	m.InputTokens += e.InputTokens
	m.OutputTokens += e.OutputTokens
	key := e.TeamID + ":" + RunKey(e.WorkflowID, e.TemporalRunID)
	a.runs[key] = true
	if e.Status == "failed" {
		m.FailedRequests++
	}
	if e.Attempt > 1 {
		m.RetryRequests++
	}
	started, _ := time.Parse(time.RFC3339Nano, e.StartedAt)
	last, _ := time.Parse(time.RFC3339Nano, a.row.LastRequestAt)
	if started.After(last) {
		a.row.LastRequestAt = e.StartedAt
	}
	if e.Accounting == "pending" {
		if started, err := time.Parse(time.RFC3339Nano, e.StartedAt); err == nil && now.Sub(started) > time.Hour {
			e.Accounting = "unknown"
		}
	}
	switch e.Accounting {
	case "estimated":
		if cost, ok := new(big.Rat).SetString(e.CostUSD); ok && cost.Sign() >= 0 {
			a.cost.Add(a.cost, cost)
			m.Priced++
			a.pricedRuns[key] = true
		} else {
			m.Unknown++
		}
	case "unpriced":
		m.Unpriced++
	case "pending":
		m.Pending++
	default:
		m.Unknown++
	}
}
func (a *accumulator) finish() AnalyticsRow {
	a.row.EstimatedUSD = a.cost.FloatString(12)
	a.row.Runs = len(a.runs)
	a.row.PricedRuns = len(a.pricedRuns)
	if a.row.PricedRuns > 0 {
		avg := new(big.Rat).Quo(a.cost, big.NewRat(int64(a.row.PricedRuns), 1)).FloatString(12)
		a.row.AverageUSD = &avg
	}
	return a.row
}
func rowsFrom(values map[string]*accumulator) []AnalyticsRow {
	rows := make([]AnalyticsRow, 0, len(values))
	for _, a := range values {
		rows = append(rows, a.finish())
	}
	SortAnalytics(rows, "cost", "desc")
	return rows
}
func bucketAt(t time.Time, bucket string) time.Time {
	if bucket == "hour" {
		return t.Truncate(time.Hour)
	}
	if bucket == "month" {
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Aggregate direct ledger entries once. Parent descendant summaries must never
// be summed here: that would charge child requests twice.
func Aggregate(entries []Entry, names map[string]string, w Window, now time.Time) Analytics {
	all := map[string]*accumulator{}
	total := accumulatorFor(all, "current", "")
	previous := accumulatorFor(all, "previous", "")
	teams, runs, models, trend := map[string]*accumulator{}, map[string]*accumulator{}, map[string]*accumulator{}, map[string]*accumulator{}
	for id, name := range names {
		accumulatorFor(teams, id, name)
	}
	for at := bucketAt(w.From, w.Bucket); at.Before(w.Until); {
		id := at.Format(time.RFC3339)
		accumulatorFor(trend, id, id)
		if w.Bucket == "hour" {
			at = at.Add(time.Hour)
		} else if w.Bucket == "month" {
			at = at.AddDate(0, 1, 0)
		} else {
			at = at.AddDate(0, 0, 1)
		}
	}
	for _, e := range entries {
		at, err := time.Parse(time.RFC3339Nano, e.StartedAt)
		if err != nil || at.Before(w.PreviousFrom) || !at.Before(w.Until) {
			continue
		}
		if at.Before(w.From) {
			previous.add(e, now)
			continue
		}
		total.add(e, now)
		accumulatorFor(teams, e.TeamID, names[e.TeamID]).add(e, now)
		run := accumulatorFor(runs, e.TeamID+":"+RunKey(e.WorkflowID, e.TemporalRunID), e.DefinitionID)
		run.row.TeamID = e.TeamID
		run.row.TeamName = names[e.TeamID]
		run.row.WorkflowID = e.WorkflowID
		run.row.TemporalRunID = e.TemporalRunID
		run.row.DefinitionID = e.DefinitionID
		run.row.DefinitionVersion = e.DefinitionVersion
		run.add(e, now)
		model := e.Model
		if model == "" {
			model = e.RequestedModel
		}
		if model == "" {
			model = "Unknown model"
		}
		accumulatorFor(models, e.Provider+":"+model, e.Provider+" / "+model).add(e, now)
		key := bucketAt(at.UTC(), w.Bucket).Format(time.RFC3339)
		accumulatorFor(trend, key, key).add(e, now)
	}
	timeline := rowsFrom(trend)
	sort.Slice(timeline, func(i, j int) bool { return timeline[i].ID < timeline[j].ID })
	return Analytics{Window: w, Summary: total.finish().Metrics, Previous: previous.finish().Metrics, Trend: timeline, Teams: rowsFrom(teams), Runs: rowsFrom(runs), Models: rowsFrom(models)}
}

func SortAnalytics(rows []AnalyticsRow, field, direction string) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		cmp := 0
		switch field {
		case "cost":
			if (a.Priced == 0) != (b.Priced == 0) {
				return a.Priced > 0
			}
			x, _ := new(big.Rat).SetString(a.EstimatedUSD)
			y, _ := new(big.Rat).SetString(b.EstimatedUSD)
			cmp = x.Cmp(y)
		case "requests":
			cmp = compare(int64(a.Requests), int64(b.Requests))
		case "tokens":
			cmp = compare(a.InputTokens+a.OutputTokens, b.InputTokens+b.OutputTokens)
		case "runs":
			cmp = compare(int64(a.Runs), int64(b.Runs))
		case "latest":
			x, _ := time.Parse(time.RFC3339Nano, a.LastRequestAt)
			y, _ := time.Parse(time.RFC3339Nano, b.LastRequestAt)
			if x.Before(y) {
				cmp = -1
			} else if x.After(y) {
				cmp = 1
			}
		default:
			if a.Name < b.Name {
				cmp = -1
			} else if a.Name > b.Name {
				cmp = 1
			}
		}
		if cmp == 0 {
			return a.ID < b.ID
		}
		if direction == "asc" {
			return cmp < 0
		}
		return cmp > 0
	})
}
func compare(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
