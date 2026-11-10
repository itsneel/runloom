package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

type failingAudit struct {
	store.Audits
	calls, failAt int
}

func (a *failingAudit) AppendAudit(v store.AuditEvent) (store.AuditEvent, error) {
	a.calls++
	if a.calls == a.failAt {
		return v, errors.New("sensitive-database-error")
	}
	return v, nil
}
func TestAuditFailureBoundaries(t *testing.T) {
	for _, at := range []int{1, 2} {
		a := &failingAudit{failAt: at}
		s := &Server{modules: store.Modules{Audits: a}}
		called := false
		response := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/studio/definitions", nil)
		s.auditRequest(response, r, "POST /api/v1/studio/definitions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			_, _ = w.Write([]byte("sensitive-output"))
		}))
		require.Equal(t, at == 2, called)
		require.Equal(t, 503, response.Code)
		require.NotContains(t, response.Body.String(), "sensitive")
	}
}
func TestSensitiveExecutionRoles(t *testing.T) {
	require.True(t, sensitiveRunAccess("admin"))
	require.False(t, sensitiveRunAccess("editor"))
	require.False(t, sensitiveRunAccess("viewer"))
	t.Setenv("RUNLOOM_SENSITIVE_RUN_ROLES", "admin,editor")
	require.True(t, sensitiveRunAccess("editor"))
	require.False(t, sensitiveRunAccess("viewer"))
}
func TestServerErrorsDoNotExposeData(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, 500, errors.New("synthetic-private-value"))
	require.NotContains(t, response.Body.String(), "synthetic-private-value")
}
