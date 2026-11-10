package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCanvasLayoutRoundTrip(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, &documentedClient{}, "test")
	defer s.Close()
	body := `{"schema_version":"1.0","id":"canvas","name":"Canvas","steps":[{"id":"echo","type":"activity","activity":"runloom.echo"}],"layout":{"positions":{"echo":{"x":-250.5,"y":425},"@trigger":{"x":0,"y":0}}}}`
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/studio/definitions", strings.NewReader(body)))
	require.Equal(t, 200, w.Code, w.Body.String())
	var saved model.Definition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, -250.5, saved.Layout.Positions["echo"].X)
	_, err = b.Publish("canvas")
	require.NoError(t, err)
	saved.Layout.Positions["echo"] = model.CanvasPoint{X: 300, Y: 400}
	_, err = b.SaveDraft(saved)
	require.NoError(t, err)
	version := 1
	published, err := b.Get("canvas", &version)
	require.NoError(t, err)
	require.Equal(t, -250.5, published.Layout.Positions["echo"].X)
	version = 0
	draft, err := b.Get("canvas", &version)
	require.NoError(t, err)
	require.Equal(t, float64(300), draft.Layout.Positions["echo"].X)
	require.Equal(t, published.Steps, draft.Steps, "layout edits must not change execution")
	for _, invalid := range []string{strings.Replace(body, "-250.5", "100001", 1), strings.Replace(body, `"@trigger"`, `"invalid/key"`, 1)} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/studio/definitions", strings.NewReader(invalid)))
		require.Equal(t, 422, w.Code, w.Body.String())
	}
	for _, point := range []string{`{"x":null,"y":1}`, `{"y":1}`, `null`, `{"x":1,"y":2,"z":3}`} {
		invalid := strings.Replace(body, `{"x":-250.5,"y":425}`, point, 1)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/studio/definitions", strings.NewReader(invalid)))
		require.Equal(t, 422, w.Code, w.Body.String())
	}
}
