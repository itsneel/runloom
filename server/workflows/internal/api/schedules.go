package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

type scheduleRequest struct {
	Name            string         `json:"name"`
	DefinitionID    string         `json:"definition_id"`
	Version         int            `json:"version"`
	Input           map[string]any `json:"input"`
	IntervalSeconds int            `json:"interval_seconds,omitempty"`
	Cron            string         `json:"cron,omitempty"`
	TimeZone        string         `json:"time_zone,omitempty"`
	At              string         `json:"at,omitempty"`
	Paused          bool           `json:"paused"`
}

type scheduleView struct {
	ID string `json:"id"`
	scheduleRequest
	NextRuns   []time.Time `json:"next_runs"`
	RecentRuns []string    `json:"recent_runs"`
}

func schedulePrefix(team string) string { return "runloom-" + team + "-" }

func scheduleSpec(body *scheduleRequest, now time.Time) (client.ScheduleSpec, error) {
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) < 1 || len(body.Name) > 120 || body.Version < 1 {
		return client.ScheduleSpec{}, fmt.Errorf("name and a published version >= 1 are required")
	}
	if body.Input == nil {
		body.Input = map[string]any{}
	}
	if body.TimeZone == "" {
		body.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(body.TimeZone); err != nil {
		return client.ScheduleSpec{}, fmt.Errorf("invalid IANA time_zone")
	}
	choices := 0
	if body.IntervalSeconds != 0 {
		choices++
	}
	if body.Cron != "" {
		choices++
	}
	if body.At != "" {
		choices++
	}
	if choices != 1 {
		return client.ScheduleSpec{}, fmt.Errorf("choose exactly one of interval_seconds, cron, or at")
	}
	spec := client.ScheduleSpec{TimeZoneName: body.TimeZone}
	switch {
	case body.IntervalSeconds != 0:
		if body.IntervalSeconds < 60 || body.IntervalSeconds > 31536000 {
			return spec, fmt.Errorf("interval_seconds must be between 60 and 31536000")
		}
		spec.Intervals = []client.ScheduleIntervalSpec{{Every: time.Duration(body.IntervalSeconds) * time.Second}}
	case body.Cron != "":
		if len(body.Cron) > 200 || len(strings.Fields(body.Cron)) != 5 {
			return spec, fmt.Errorf("cron must contain five fields: minute hour day month weekday")
		}
		spec.CronExpressions = []string{body.Cron}
	case body.At != "":
		at, err := time.Parse(time.RFC3339, body.At)
		if err != nil || !at.After(now) {
			return spec, fmt.Errorf("at must be a future RFC3339 timestamp with a time zone")
		}
		at = at.UTC().Truncate(time.Second)
		if !at.After(now) {
			return spec, fmt.Errorf("at must be at least one second in the future")
		}
		field := func(value int) []client.ScheduleRange { return []client.ScheduleRange{{Start: value, End: value}} }
		spec.TimeZoneName = "UTC"
		spec.Calendars = []client.ScheduleCalendarSpec{{Year: field(at.Year()), Month: field(int(at.Month())), DayOfMonth: field(at.Day()), Hour: field(at.Hour()), Minute: field(at.Minute()), Second: field(at.Second())}}
		spec.StartAt, spec.EndAt = at, at.Add(time.Second)
		body.At = at.Format(time.RFC3339)
	}
	return spec, nil
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var body scheduleRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	spec, err := scheduleSpec(&body, time.Now())
	if err != nil {
		writeError(w, 422, err)
		return
	}
	value, err := s.modules.Catalog.Get(body.DefinitionID, &body.Version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := definition.ValidateInput(value.InputSchema, body.Input); err != nil {
		writeError(w, 422, err)
		return
	}
	input, err := s.prepareRun(r, value, body.Input)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	team, err := uuid.Parse(input.TeamID)
	if err != nil || team.String() != input.TeamID {
		writeError(w, 400, fmt.Errorf("a valid team is required"))
		return
	}
	id := schedulePrefix(input.TeamID) + uuid.NewString()
	input.Source, input.ScheduleID = "schedule", id
	tagRun(&input, "", "schedule", false)
	options := client.ScheduleOptions{ID: id, Spec: spec,
		Action: &client.ScheduleWorkflowAction{ID: input.TeamID + "-schedule-" + uuid.NewString(), Workflow: model.WorkflowType("RunloomWorkflow", input), TaskQueue: s.taskQueue, Args: []any{input}, TypedSearchAttributes: input.Metadata.SearchAttributes()},
		Paused: body.Paused, Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: 15 * time.Minute, PauseOnFailure: true, Memo: map[string]any{"runloom_schedule": body},
	}
	if body.At != "" {
		options.RemainingActions = 1
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err = s.temporal.ScheduleClient().Create(ctx, options); err != nil {
		writeScheduleError(w, err)
		return
	}
	s.audit(store.AuditEvent{DefinitionID: value.ID, EventType: "schedule.created", Actor: requestActor(r, "api-user"), Payload: map[string]any{"schedule_id": id, "definition_version": value.Version, "name": body.Name}})
	writeJSON(w, 201, scheduleView{ID: id, scheduleRequest: body, NextRuns: []time.Time{}, RecentRuns: []string{}})
}

func scheduleFromMemo(id string, memo *commonpb.Memo, paused bool, next []time.Time, recent []client.ScheduleActionResult) (scheduleView, error) {
	view := scheduleView{ID: id, NextRuns: next, RecentRuns: []string{}}
	if memo == nil || memo.Fields["runloom_schedule"] == nil {
		return view, fmt.Errorf("schedule metadata missing")
	}
	dc, err := protection.DataConverter()
	if err != nil {
		return view, err
	}
	if err := dc.FromPayload(memo.Fields["runloom_schedule"], &view.scheduleRequest); err != nil {
		return view, err
	}
	view.Paused = paused
	for _, action := range recent {
		if action.StartWorkflowResult != nil {
			view.RecentRuns = append(view.RecentRuns, action.StartWorkflowResult.WorkflowID)
		}
	}
	return view, nil
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	it, err := s.temporal.ScheduleClient().List(ctx, client.ScheduleListOptions{PageSize: 100})
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	values := []scheduleView{}
	prefix := schedulePrefix(r.Header.Get("X-Runloom-Team"))
	for scanned := 0; it.HasNext(); scanned++ {
		if scanned >= 10000 {
			writeError(w, 503, fmt.Errorf("schedule listing exceeded namespace scan limit"))
			return
		}
		item, err := it.Next()
		if err != nil {
			writeScheduleError(w, err)
			return
		}
		if !strings.HasPrefix(item.ID, prefix) {
			continue
		}
		view, err := scheduleFromMemo(item.ID, item.Memo, item.Paused, item.NextActionTimes, item.RecentActions)
		if err != nil {
			writeError(w, 503, err)
			return
		}
		values = append(values, view)
	}
	writeJSON(w, 200, values)
}

func (s *Server) scheduleHandle(w http.ResponseWriter, r *http.Request) client.ScheduleHandle {
	id := r.PathValue("schedule")
	prefix := schedulePrefix(r.Header.Get("X-Runloom-Team"))
	if !strings.HasPrefix(id, prefix) {
		writeError(w, 404, fmt.Errorf("schedule not found in this team"))
		return nil
	}
	if _, err := uuid.Parse(strings.TrimPrefix(id, prefix)); err != nil {
		writeError(w, 404, fmt.Errorf("schedule not found"))
		return nil
	}
	return s.temporal.ScheduleClient().GetHandle(r.Context(), id)
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	handle := s.scheduleHandle(w, r)
	if handle == nil {
		return
	}
	d, err := handle.Describe(r.Context())
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	view, err := scheduleFromMemo(handle.GetID(), d.Memo, d.Schedule.State != nil && d.Schedule.State.Paused, d.Info.NextActionTimes, d.Info.RecentActions)
	if err != nil {
		writeError(w, 503, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) changeSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	handle := s.scheduleHandle(w, r)
	if handle == nil {
		return
	}
	var err error
	action := r.PathValue("action")
	if r.Method == http.MethodDelete {
		action = "delete"
	}
	switch action {
	case "pause":
		err = handle.Pause(r.Context(), client.SchedulePauseOptions{Note: "Paused in Runloom"})
	case "resume":
		err = handle.Unpause(r.Context(), client.ScheduleUnpauseOptions{Note: "Resumed in Runloom"})
	case "trigger":
		err = handle.Trigger(r.Context(), client.ScheduleTriggerOptions{Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP})
	case "delete":
		err = handle.Delete(r.Context())
	default:
		writeError(w, 404, fmt.Errorf("unknown schedule action"))
		return
	}
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	s.audit(store.AuditEvent{EventType: "schedule." + action, Actor: requestActor(r, "api-user"), Payload: map[string]string{"schedule_id": handle.GetID()}})
	writeJSON(w, 200, map[string]string{"schedule_id": handle.GetID(), "action": action})
}

func writeScheduleError(w http.ResponseWriter, err error) {
	var missing *serviceerror.NotFound
	var invalid *serviceerror.InvalidArgument
	switch {
	case errors.As(err, &missing):
		writeError(w, 404, fmt.Errorf("schedule not found"))
	case errors.As(err, &invalid):
		writeError(w, 422, err)
	default:
		writeError(w, 503, fmt.Errorf("schedule service unavailable; retry"))
	}
}
