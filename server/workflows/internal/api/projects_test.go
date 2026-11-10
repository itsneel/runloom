package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestProjectAPI(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "test")
	defer s.Close()
	call := func(method, path, body, project string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Project", project)
		r.Header.Set("X-Runloom-Actor", "project-tester")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	response := call("GET", "/api/v1/studio/projects", "", "", 200)
	var projects []store.Project
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projects))
	require.Equal(t, "General", projects[0].Name)
	response = call("POST", "/api/v1/studio/projects", `{"name":"Finance"}`, "", 201)
	var project store.Project
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &project))
	call("POST", "/api/v1/studio/projects", `{"name":"finance"}`, "", 409)
	call("POST", "/api/v1/studio/projects", `{"name":" "}`, "", 400)
	call("POST", "/api/v1/studio/projects", `{"name":"Invalid", "unknown":true}`, "", 400)
	call("POST", "/api/v1/studio/projects/"+project.ID+"/rename", `{"name":"Operations"}`, "", 200)
	call("POST", "/api/v1/studio/projects/missing/rename", `{"name":"Missing"}`, "", 404)
	definition := `{"schema_version":"1.0","id":"project-flow","name":"Project workflow","trigger":{"type":"manual"},"steps":[{"id":"echo","type":"activity","activity":"runloom.echo"}]}`
	call("POST", "/api/v1/studio/definitions", definition, "missing", 404)
	call("GET", "/api/v1/studio/definitions/project-flow?version=0", "", "", 404)
	call("POST", "/api/v1/studio/definitions", definition, project.ID, 200)
	response = call("GET", "/api/v1/studio/definitions?project_id="+project.ID, "", "", 200)
	var definitions []store.Summary
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &definitions))
	require.Len(t, definitions, 1)
	require.Equal(t, project.ID, definitions[0].ProjectID)
	call("GET", "/api/v1/studio/definitions?project_id=missing", "", "", 404)
	response = call("GET", "/api/v1/studio/definitions?project_id=default", "", "", 200)
	require.JSONEq(t, `[]`, response.Body.String())
	call("POST", "/api/v1/studio/definitions/project-flow/publish", `{}`, "", 200)
	before := call("GET", "/api/v1/studio/definitions/project-flow?version=1", "", "", 200).Body.String()
	call("POST", "/api/v1/studio/definitions/project-flow/project", `{"project_id":"default"}`, "", 200)
	call("POST", "/api/v1/studio/definitions/project-flow/project", `{"project_id":"missing"}`, "", 404)
	call("POST", "/api/v1/studio/definitions/missing/project", `{"project_id":"default"}`, "", 404)
	after := call("GET", "/api/v1/studio/definitions/project-flow?version=1", "", "", 200).Body.String()
	require.JSONEq(t, before, after)
	response = call("GET", "/api/v1/studio/definitions?project_id=default", "", "", 200)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &definitions))
	require.Len(t, definitions, 1)
	require.Equal(t, "default", definitions[0].ProjectID)
	response = call("GET", "/api/v1/studio/audit", "", "", 200)
	require.Contains(t, response.Body.String(), "project.created")
	require.Contains(t, response.Body.String(), "project.renamed")
	require.Contains(t, response.Body.String(), "definition.project_moved")
	require.Contains(t, response.Body.String(), "project-tester")
}
