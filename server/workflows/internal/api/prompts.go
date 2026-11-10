package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/store"
)

func (s *Server) hydratePrompts(r *http.Request, d model.Definition) (model.Definition, error) {
	return definition.ResolvePrompts(d, func(id string, v int) (prompts.Entry, error) { return s.modules.Prompts.GetPrompt(r.Context(), id, v) })
}
func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	v, err := s.modules.Prompts.ListPrompts(r.Context())
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) getPrompt(w http.ResponseWriter, r *http.Request) {
	v := 0
	var err error
	if raw := r.URL.Query().Get("version"); raw != "" {
		v, err = strconv.Atoi(raw)
	}
	if err != nil || v < 0 {
		writeError(w, 422, fmt.Errorf("invalid prompt version"))
		return
	}
	e, err := s.modules.Prompts.GetPrompt(r.Context(), r.PathValue("id"), v)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, 200, e)
}
func (s *Server) promptHistory(w http.ResponseWriter, r *http.Request) {
	e, err := s.modules.Prompts.PromptHistory(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, 200, e)
}
func (s *Server) savePrompt(w http.ResponseWriter, r *http.Request) {
	var e prompts.Entry
	if err := decodeJSON(w, r, &e); err != nil {
		writeError(w, 422, err)
		return
	}
	if e.ID != r.PathValue("id") {
		writeError(w, 422, fmt.Errorf("prompt ID mismatch"))
		return
	}
	publish := r.PathValue("action") == "publish"
	if r.PathValue("action") != "save" && !publish {
		writeError(w, 404, fmt.Errorf("unknown prompt action"))
		return
	}
	next, err := s.modules.Prompts.SavePrompt(r.Context(), e, publish, requestActor(r, "studio-user"))
	if err != nil {
		code := 500
		if errors.Is(err, store.ErrInvalidPrompt) {
			code = 422
		}
		if errors.Is(err, store.ErrPromptConflict) {
			code = 409
		}
		writeError(w, code, err)
		return
	}
	writeJSON(w, 200, next)
}
func (s *Server) previewPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Template prompts.Template `json:"template"`
		Inputs   map[string]any   `json:"inputs"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 422, err)
		return
	}
	out, err := prompts.Render(body.Template, body.Inputs)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) variableHints(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Definition model.Definition `json:"definition"`
		StepID     string           `json:"step_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 422, err)
		return
	}
	if len(body.Definition.Steps) > 500 {
		writeError(w, 422, fmt.Errorf("too many nodes"))
		return
	}
	out, err := definition.VariableHints(body.Definition, body.StepID)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	writeJSON(w, 200, out)
}

// Prompt tests use the ordinary draft-test path, preserving the real workflow's
// team/project identity, budget checks, accounting and run permissions.
func (s *Server) testPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DefinitionID string           `json:"definition_id"`
		Template     prompts.Template `json:"template"`
		Inputs       map[string]any   `json:"inputs"`
		Model        string           `json:"model"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 422, err)
		return
	}
	if _, err := prompts.Render(body.Template, body.Inputs); err != nil {
		writeError(w, 422, err)
		return
	}
	v := 0
	d, err := s.modules.Catalog.Get(body.DefinitionID, &v)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	mappings := map[string]any{}
	for key := range body.Inputs {
		mappings[key] = "${input." + key + "}"
	}
	d.Steps = []model.Step{{ID: "prompt-test", Type: "llm", Model: body.Model, PromptTemplate: &body.Template, PromptInputs: mappings}}
	d.InputSchema = map[string]any{}
	d.Output = "${steps.prompt-test.output}"
	d.Layout = nil
	raw, _ := json.Marshal(map[string]any{"definition": d, "input": body.Inputs})
	request := r.Clone(r.Context())
	request.Body = http.NoBody
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw))
	request.SetPathValue("id", d.ID)
	s.testWorkflow(w, request)
}
