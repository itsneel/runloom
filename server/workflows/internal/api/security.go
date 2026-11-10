package api

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/example/runloom/workflows/internal/config"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
)

// Authoring permission is not permission to inspect execution payloads. The
// role allowlist is installation policy and cannot be overridden by the caller.
func sensitiveRunAccess(role string) bool {
	roles := config.Value("RUNLOOM_SENSITIVE_RUN_ROLES")
	if roles == "" {
		roles = "admin"
	}
	for _, allowed := range strings.Split(roles, ",") {
		if strings.TrimSpace(allowed) == role {
			return true
		}
	}
	return false
}

type auditResponse struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (w *auditResponse) Header() http.Header { return w.header }
func (w *auditResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *auditResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	if w.body.Len()+len(p) > 32<<20 {
		w.overflow = true
		return 0, fmt.Errorf("response limit exceeded")
	}
	return w.body.Write(p)
}

// Persist intent before any business side effect, then persist the outcome
// before releasing a response. An intent without an outcome means UNKNOWN,
// never success. This does not make Temporal and the database one transaction.
func (s *Server) audited(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, route := mux.Handler(r)
		if !strings.HasPrefix(r.URL.Path, "/api/v1/studio/") || route == "" {
			mux.ServeHTTP(w, r)
			return
		}
		s.auditRequest(w, r, route, mux)
	})
}
func (s *Server) auditRequest(w http.ResponseWriter, r *http.Request, route string, next http.Handler) {
	if s.modules.Audits == nil {
		writeError(w, 503, fmt.Errorf("security audit unavailable"))
		return
	}
	id := uuid.NewString()
	w.Header().Set("X-Runloom-Request-ID", id)
	actor := requestActor(r, "system")
	event := store.AuditEvent{EventID: id, EventType: "security.request_authorized", Actor: actor, Payload: map[string]any{"request_id": id, "route": route, "method": r.Method}}
	if _, err := s.modules.Audits.AppendAudit(event); err != nil {
		writeError(w, 503, fmt.Errorf("security audit unavailable"))
		return
	}
	response := &auditResponse{header: make(http.Header)}
	next.ServeHTTP(response, r)
	if response.status == 0 {
		response.status = 200
	}
	if response.overflow {
		response.status = 500
	}
	event.EventID = uuid.NewString()
	event.EventType = "security.request_completed"
	event.Payload = map[string]any{"request_id": id, "route": route, "method": r.Method, "status": response.status}
	if _, err := s.modules.Audits.AppendAudit(event); err != nil {
		log.Printf("security audit completion unavailable; request_id=%s outcome=unknown", id)
		writeError(w, 503, fmt.Errorf("audit completion unavailable; operation may have completed"))
		return
	}
	if response.overflow {
		writeError(w, 500, fmt.Errorf("response limit exceeded"))
		return
	}
	for key, values := range response.header {
		w.Header()[key] = values
	}
	w.Header().Set("X-Runloom-Request-ID", id)
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body.Bytes())
}
