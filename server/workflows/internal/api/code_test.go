package api

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCodePreviewAndAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runloom-code")
	out, err := exec.Command("go", "build", "-o", path, "../../cmd/code").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Setenv("RUNLOOM_CODE_RUNNER", path)
	backend, err := store.New(t.TempDir())
	require.NoError(t, err)
	server := New(backend, nil, "test")
	defer server.Close()
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/studio/code/test", strings.NewReader(body))
		r.Header.Set("X-Runloom-Team", "team-a")
		r.Header.Set("X-Runloom-Actor", "editor")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	w := call(`{"code":{"runtime":"quickjs-ng-0.11.0-v1","source":"return {total: input.amount * 2}"},"input":{"amount":21}}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `{"output":{"total":42}}`, w.Body.String())
	w = call(`{"code":{"runtime":"quickjs-ng-0.11.0-v1","source":"throw new Error('example failure')"},"input":{}}`)
	require.Equal(t, 422, w.Code)
	require.Contains(t, w.Body.String(), "example failure")
	for _, body := range []string{`{}`, `{"code":{"runtime":"typescript","source":"return 1"}}`, `{"code":{"runtime":"quickjs-ng-0.11.0-v1","source":"return 1"},"input":[]}`, `{} {}`, `{"team_id":"spoofed"}`} {
		require.Equal(t, 422, call(body).Code, body)
	}
	events, err := backend.ListAudit("", "", 100)
	require.NoError(t, err)
	codeEvents := 0
	for _, event := range events {
		if event.EventType == "code.tested" {
			codeEvents++
		} else {
			require.Contains(t, []string{"security.request_authorized", "security.request_completed"}, event.EventType)
		}
		require.Equal(t, "editor", event.Actor)
	}
	require.Equal(t, 2, codeEvents)
	raw, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "amount")
	require.NotContains(t, string(raw), "example failure")
}
