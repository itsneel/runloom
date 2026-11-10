package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

type scheduleTestClient struct {
	client.Client
	schedules client.ScheduleClient
}

func (c scheduleTestClient) ScheduleClient() client.ScheduleClient { return c.schedules }

type scheduleTestList struct {
	client.ScheduleClient
	items []*client.ScheduleListEntry
}

func (s scheduleTestList) List(context.Context, client.ScheduleListOptions) (client.ScheduleListIterator, error) {
	return &scheduleTestIterator{items: s.items}, nil
}

type scheduleTestIterator struct{ items []*client.ScheduleListEntry }

func (i *scheduleTestIterator) HasNext() bool { return len(i.items) > 0 }
func (i *scheduleTestIterator) Next() (*client.ScheduleListEntry, error) {
	v := i.items[0]
	i.items = i.items[1:]
	return v, nil
}

func TestScheduleListingFiltersBeforeReadingMetadata(t *testing.T) {
	team := uuid.NewString()
	id := schedulePrefix(team) + uuid.NewString()
	payload, err := converter.GetDefaultDataConverter().ToPayload(scheduleRequest{Name: "Own schedule", Version: 1, Input: map[string]any{"private": "own-data"}})
	require.NoError(t, err)
	schedules := scheduleTestList{items: []*client.ScheduleListEntry{
		{ID: schedulePrefix(uuid.NewString()) + uuid.NewString()}, // No valid memo: must be ignored, not decoded.
		{ID: id, Memo: &commonpb.Memo{Fields: map[string]*commonpb.Payload{"runloom_schedule": payload}}},
	}}
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := &Server{modules: store.Bind(b), temporal: scheduleTestClient{schedules: schedules}, mux: http.NewServeMux()}
	s.routes()
	req := httptest.NewRequest("GET", "/api/v1/studio/schedules", nil)
	req.Header.Set("X-Runloom-Team", team)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	require.Equal(t, 200, response.Code, response.Body.String())
	var values []scheduleView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &values))
	require.Len(t, values, 1)
	require.Equal(t, id, values[0].ID)
}

func TestScheduleSpecification(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		request scheduleRequest
		valid   bool
	}{
		{"interval", scheduleRequest{IntervalSeconds: 60}, true},
		{"calendar", scheduleRequest{Cron: "0 9 * * 1-5", TimeZone: "Asia/Kolkata"}, true},
		{"once", scheduleRequest{At: "2026-01-02T09:00:00+05:30"}, true},
		{"missing", scheduleRequest{}, false},
		{"too fast", scheduleRequest{IntervalSeconds: 59}, false},
		{"multiple", scheduleRequest{IntervalSeconds: 60, Cron: "0 9 * * *"}, false},
		{"zone", scheduleRequest{Cron: "0 9 * * *", TimeZone: "wrong/zone"}, false},
		{"seconds cron", scheduleRequest{Cron: "0 0 9 * * *"}, false},
		{"past", scheduleRequest{At: "2025-12-31T09:00:00Z"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.request.Name = "test"
			tc.request.Version = 1
			spec, err := scheduleSpec(&tc.request, now)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.name == "once" {
				require.Equal(t, "UTC", spec.TimeZoneName)
				require.Equal(t, 3, spec.Calendars[0].Hour[0].Start)
				require.Equal(t, 30, spec.Calendars[0].Minute[0].Start)
			}
		})
	}
}

func TestSchedulesRejectOtherTeamsBeforeTemporalLookup(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "test")
	defer s.Close()
	for _, method := range []string{"GET", "POST", "DELETE"} {
		path := "/api/v1/studio/schedules/" + schedulePrefix(uuid.NewString()) + uuid.NewString()
		if method == "POST" {
			path += "/trigger"
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-Runloom-Team", uuid.NewString())
		result := httptest.NewRecorder()
		s.Handler().ServeHTTP(result, req)
		require.Equal(t, 404, result.Code)
	}
}
