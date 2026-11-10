package api

import (
	"errors"
	"fmt"

	"net/http"
	"strconv"

	"github.com/example/runloom/management/internal/prompts"
	"github.com/example/runloom/management/internal/store"
)

func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	v, err := s.backend.ListPrompts(r.Context())
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
	e, err := s.backend.GetPrompt(r.Context(), r.PathValue("id"), v)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) promptHistory(w http.ResponseWriter, r *http.Request) {
	e, err := s.backend.PromptHistory(r.Context(), r.PathValue("id"))
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
	next, err := s.backend.SavePrompt(r.Context(), e, publish, requestActor(r, "studio-user"))
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
