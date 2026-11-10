package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestListRunsTagFilters(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "test")
	defer s.Close()
	_, err = b.RecordRunStart(store.RunRecord{WorkflowID: "run", Metadata: &model.ExecutionMetadata{ProjectID: "project", IsTest: true}})
	require.NoError(t, err)
	for _, query := range []string{"?tag_key=is_test&tag_value=true", "?search=PROJECT", "?offset=0&limit=1"} {
		w := httptest.NewRecorder()
		s.listRuns(w, httptest.NewRequest("GET", "/api/v1/studio/runs"+query, nil))
		require.Equal(t, 200, w.Code)
		var rows []store.RunRecord
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
		require.Len(t, rows, 1)
		require.Equal(t, "project", rows[0].Metadata.ProjectID)
	}
	for _, query := range []string{"?tag_key=source", "?tag_value=manual", "?tag_key=secret&tag_value=x", "?offset=-1", "?offset=no", "?tag_key=is_test&tag_value=1", "?tag_key=definition_version&tag_value=-1"} {
		w := httptest.NewRecorder()
		s.listRuns(w, httptest.NewRequest("GET", "/api/v1/studio/runs"+query, nil))
		require.Equal(t, 400, w.Code, query)
	}
}
