package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/codeexec"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
)

// Gateway applies interactive Studio authentication and team editor permissions.
// Consumers have no route to this endpoint. Inputs and source are never audited.
func (s *Server) testCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code  model.JavaScriptCode `json:"code"`
		Input map[string]any       `json:"input"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*codecontract.MaxJSONBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, 422, fmt.Errorf("invalid JavaScript test request"))
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 422, fmt.Errorf("expected a single JSON request"))
		return
	}
	input := model.CodeActivityInput{TeamID: r.Header.Get("X-Runloom-Team"), Code: body.Code, Input: body.Input}
	if err := codecontract.ValidateInput(input); err != nil {
		writeError(w, 422, err)
		return
	}
	output, err := codeexec.Execute(r.Context(), input)
	status := "completed"
	if err != nil {
		status = "failed"
	}
	s.audit(store.AuditEvent{EventType: "code.tested", Actor: requestActor(r, "studio-user"), Payload: map[string]any{"runtime": body.Code.Runtime, "status": status}})
	if err != nil {
		code := http.StatusUnprocessableEntity
		if errors.Is(err, codeexec.ErrBusy) {
			code = http.StatusTooManyRequests
			w.Header().Set("Retry-After", "1")
		}
		if errors.Is(err, codeexec.ErrUnavailable) {
			code = http.StatusServiceUnavailable
		}
		writeError(w, code, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}
