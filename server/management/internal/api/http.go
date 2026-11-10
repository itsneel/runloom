package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/example/runloom/management/internal/store"
	"io"
	"net/http"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusUnprocessableEntity, err)
}

func writeError(w http.ResponseWriter, code int, err error) {
	if code >= 500 {
		err = fmt.Errorf("service operation failed; retry or contact an administrator")
	}
	writeJSON(w, code, map[string]string{"detail": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func requestActor(r *http.Request, fallback string) string {
	value := r.Header.Get("X-Runloom-Actor")
	if value == "" {
		return fallback
	}
	if len(value) > 200 {
		return value[:200]
	}
	return value
}
