package services

import (
	"github.com/example/runloom/auth/internal/access"
	"net/http"
	"strings"
)

type Internal struct {
	Trust     Trust
	Directory *access.Directory
	Verifier  access.Verifier
}

func (h *Internal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	caller := h.Trust.caller(r)
	if caller == "" {
		http.Error(w, "service authentication required", 401)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/")
	if caller == "workflows" || caller == "management" {
		switch path {
		case "identity/bind":
			var b struct{ Token string }
			if !decode(w, r, &b) {
				return
			}
			user, err := h.Verifier.Verify(r.Context(), b.Token)
			if err != nil {
				reply(w, nil, access.ErrForbidden)
				return
			}
			state, err := h.Directory.Bind(r.Context(), user)
			state.Secrets = nil
			if caller == "management" {
				state.ServiceAccounts = nil
			}
			reply(w, state, err)
			return
		case "identity/view":
			var b struct{ Scope string }
			if !decode(w, r, &b) {
				return
			}
			if b.Scope != "identity" && (b.Scope != "consumers" || caller != "workflows") {
				reply(w, nil, access.ErrForbidden)
				return
			}
			state, _, err := h.Directory.Repo.Module(b.Scope).Read(r.Context())
			state.Secrets = nil
			if caller == "management" {
				state.ServiceAccounts = nil
			}
			reply(w, state, err)
			return
		}
	}

	http.NotFound(w, r)
}
