package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIDocsPublicRoutesAndAuthentication(t *testing.T) {
	f := newConsumerFixture(t)
	for _, path := range []string{"/api/docs/studio/", "/api/docs/consumer/", "/api/v1/studio/openapi.json", "/api/v1/openapi.json", "/api/docs/assets/docs.js", "/api/docs/assets/docs.css", "/api/docs/assets/swagger-ui-bundle.js", "/api/docs/assets/swagger-ui.css", "/api/docs/assets/LICENSE", "/api/docs/assets/NOTICE"} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := f.call(method, path, "", "", nil)
				require.Equal(t, 200, response.Code)
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
				require.Contains(t, response.Header().Get("Content-Security-Policy"), "connect-src 'self'")
				if method == http.MethodHead {
					require.Empty(t, response.Body.String())
				} else {
					require.NotEmpty(t, response.Body.String())
				}
			}
			response := f.call(http.MethodPost, path, "", "", nil)
			require.Equal(t, 405, response.Code)
			require.Equal(t, "GET, HEAD", response.Header().Get("Allow"))
		})
	}
	for _, path := range []string{"/api/docs", "/api/docs/", "/api/docs/studio", "/api/docs/consumer"} {
		response := f.call("GET", path, "", "", nil)
		require.Equal(t, http.StatusTemporaryRedirect, response.Code)
		require.True(t, strings.HasSuffix(response.Header().Get("Location"), "/"))
	}
	for _, path := range []string{"/api/v1/studio/runs", "/api/v1/studio/me", "/api/v1/studio/openapi.json/extra", "/api/v1/runs"} {
		require.Equal(t, 401, f.call("GET", path, "", "", nil).Code, path)
	}
	account := f.account()
	require.Equal(t, 401, f.call("GET", "/api/v1/studio/runs", account.ClientID, "", nil).Code)
	for _, action := range []string{"pause", "stop", "resume", "terminate"} {
		require.Equal(t, 404, f.call("POST", "/api/v1/runs/example/"+action, account.ClientID, "", nil).Code)
	}
	for _, path := range []string{"/api/docs/assets/../studio.openapi.json", "/api/docs/assets/private", "/api/v2/studio/openapi.json", "/api/v1/studio/openapi.json/extra"} {
		response := httptest.NewRecorder()
		require.False(t, serveAPIDocs(response, httptest.NewRequest("GET", path, nil)), path)
	}
}

func TestStudioOpenAPIRouteCoverage(t *testing.T) {
	raw, err := documentation.ReadFile("studio.openapi.json")
	require.NoError(t, err)
	var spec struct {
		OpenAPI  string                                `json:"openapi"`
		Paths    map[string]map[string]json.RawMessage `json:"paths"`
		Security []map[string][]string                 `json:"security"`
	}
	require.NoError(t, json.Unmarshal(raw, &spec))
	require.Equal(t, "3.1.0", spec.OpenAPI)
	require.Equal(t, []map[string][]string{{"studioBearer": {}}}, spec.Security)
	registered := map[string]bool{}
	route := regexp.MustCompile(`HandleFunc\("(GET|POST|PUT|DELETE) /api/v1/studio([^\"]*)"`)
	for _, file := range []string{"server.go", "gateway.go"} {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, match := range route.FindAllStringSubmatch(string(body), -1) {
			registered[strings.ToLower(match[1])+" "+match[2]] = true
		}
	}
	for _, method := range []string{"get", "post", "put", "delete"} {
		registered[method+" /definitions/{id}/edit-lock"] = true
	}
	// Auth routes are maintained as a wire-contract snapshot by the docs tool.
	// Service tests never read or build a sibling project's source.
	authRoutesJSON, err := os.ReadFile("testdata/auth-studio-routes.json")
	require.NoError(t, err)
	var authRoutes []string
	require.NoError(t, json.Unmarshal(authRoutesJSON, &authRoutes))
	for _, route := range authRoutes {
		registered[route] = true
	}
	documented := map[string]bool{}
	ids := map[string]bool{}
	for path, methods := range spec.Paths {
		for method, rawOperation := range methods {
			var op struct {
				ID          string `json:"operationId"`
				Description string `json:"description"`
				Parameters  []struct {
					Name, In string
					Required bool
				} `json:"parameters"`
				Responses map[string]any `json:"responses"`
			}
			require.NoError(t, json.Unmarshal(rawOperation, &op))
			require.NotEmpty(t, op.ID)
			require.False(t, ids[op.ID], "duplicate operation ID %s", op.ID)
			ids[op.ID] = true
			require.Contains(t, op.Description, "Required access:")
			require.Contains(t, op.Responses, "401")
			require.Contains(t, op.Responses, "403")
			if path != "/me" && !strings.HasPrefix(path, "/orgs") {
				found := false
				for _, param := range op.Parameters {
					if param.Name == "X-Runloom-Team" && param.In == "header" && param.Required {
						found = true
					}
				}
				require.True(t, found, "missing team header %s %s", method, path)
			}
			documented[method+" "+path] = true
		}
	}
	require.Equal(t, registered, documented, "Swagger must track every Studio route, without documenting invented operations")
	require.NotContains(t, spec.Paths, "/runs/{id}/pause")
	require.NotContains(t, spec.Paths, "/runs/{id}/stop")
}

func TestSwaggerBrowser(t *testing.T) {
	if os.Getenv("RUNLOOM_PLAYWRIGHT_MODULE") == "" {
		t.Skip("set RUNLOOM_PLAYWRIGHT_MODULE to enable isolated browser verification")
	}
	f := newConsumerFixture(t)
	server := httptest.NewServer(f.g)
	defer server.Close()
	cmd := exec.Command("node", "../../../api-docs/browser.mjs")
	cmd.Env = append(os.Environ(), "RUNLOOM_DOCS_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}
