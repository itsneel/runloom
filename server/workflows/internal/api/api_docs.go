package api

import (
	"embed"
	"net/http"
	"strings"
)

// Local assets and specifications make documentation available in native,
// distroless Docker and Helm installs without a CDN or Node runtime.
//
//go:embed docs/* studio.openapi.json consumer.openapi.json
var documentation embed.FS

// Exact public documentation routes only. This must not bypass authentication
// for any executable Studio or consumer endpoint.
func serveAPIDocs(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	file, contentType := "", ""
	switch path {
	case "/api/docs", "/api/docs/", "/api/docs/studio", "/api/docs/consumer", "/api/docs/platform":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		destination := "/api/docs/studio/"
		if path == "/api/docs/consumer" {
			destination = "/api/docs/consumer/"
		}
		if path == "/api/docs/platform" {
			destination = "/api/docs/platform/"
		}
		http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
		return true
	case "/api/docs/studio/", "/api/docs/consumer/", "/api/docs/platform/":
		file, contentType = "docs/index.html", "text/html; charset=utf-8"
	case "/api/v1/studio/openapi.json":
		file, contentType = "studio.openapi.json", "application/json"
	case "/api/v1/openapi.json":
		file, contentType = "consumer.openapi.json", "application/json"
	case "/api/docs/assets/swagger-ui-bundle.js", "/api/docs/assets/docs.js":
		file, contentType = "docs/"+strings.TrimPrefix(path, "/api/docs/assets/"), "application/javascript; charset=utf-8"
	case "/api/docs/assets/swagger-ui.css", "/api/docs/assets/docs.css":
		file, contentType = "docs/"+strings.TrimPrefix(path, "/api/docs/assets/"), "text/css; charset=utf-8"
	case "/api/docs/assets/LICENSE", "/api/docs/assets/NOTICE", "/api/docs/assets/swagger-ui-bundle.js.LICENSE.txt":
		file, contentType = "docs/"+strings.TrimPrefix(path, "/api/docs/assets/"), "text/plain; charset=utf-8"
	default:
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	data, err := documentation.ReadFile(file)
	if err != nil {
		http.Error(w, "Documentation unavailable", http.StatusInternalServerError)
		return true
	}
	w.Header().Set("Content-Type", contentType)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
	return true
}
