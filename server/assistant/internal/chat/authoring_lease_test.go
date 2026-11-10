package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthoringLeaseForwardedOnlyForScopedWrites(t *testing.T) {
	for _, tc := range []struct {
		name, workflow string
		write, want    bool
	}{{"root write", "root", true, true}, {"root read", "root", false, false}, {"general write", "", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.Header.Get("X-Runloom-Edit-Token") == "session-lease") != tc.want {
					t.Error("lease crossed the wrong request boundary")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer upstream.Close()
			request := httptest.NewRequest("POST", "http://assistant.test/", nil)
			request.Header.Set("X-Runloom-Edit-Token", "session-lease")
			var body any
			if tc.write {
				body = map[string]bool{"approved": true}
			}
			var result map[string]any
			if err := NewAuthoringClient(upstream.URL).call(context.Background(), request, Scope{Workflow: tc.workflow}, body, &result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
