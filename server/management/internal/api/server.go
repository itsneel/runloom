package api

import (
	"github.com/example/runloom/management/internal/store"
	"net/http"
)

type Server struct {
	backend store.View
	mux     *http.ServeMux
}

func New(b store.View) *Server {
	s := &Server{backend: b, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/v1/studio/prompts", s.listPrompts)
	s.mux.HandleFunc("GET /api/v1/studio/prompts/{id}", s.getPrompt)
	s.mux.HandleFunc("GET /api/v1/studio/prompts/{id}/history", s.promptHistory)
	s.mux.HandleFunc("POST /api/v1/studio/prompts/{id}/{action}", s.savePrompt)
	s.mux.HandleFunc("POST /api/v1/studio/prompts/preview", s.previewPrompt)
	return s
}
func (s *Server) Handler() http.Handler { return s.mux }
