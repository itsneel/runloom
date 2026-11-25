package rexeralien

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientBoundary(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(secret, []byte("synthetic-local-token"), 0600); err != nil {
		t.Fatal(err)
	}
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-local-token" {
			t.Error("missing service credential")
		}
		key = r.Header.Get("Idempotency-Key")
		if r.URL.Path == "/invalid" {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"accepted": true}, "accepted": true})
	}))
	defer server.Close()
	c := &Client{AllowHTTP: true, R2URL: server.URL, R2TokenFile: secret, QAURL: server.URL, QATokenFile: secret}
	result, err := c.R2(context.Background(), "/read", map[string]any{})
	if err != nil || result["accepted"] != true {
		t.Fatalf("R2 envelope: %v", err)
	}
	_, err = c.QA(context.Background(), map[string]any{"document_id": "synthetic"}, "stable-handoff")
	if err != nil || key != "stable-handoff" {
		t.Fatalf("QA idempotency: %v", err)
	}
	c.AllowHTTP = false
	_, err = c.R2(context.Background(), "/read", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatal("HTTP must require explicit local configuration")
	}
	c.AllowHTTP = true
	_, err = c.R2(context.Background(), "/invalid", nil)
	if statusCode(err) != 503 || strings.Contains(err.Error(), "synthetic-local-token") {
		t.Fatal("service failures must omit credentials")
	}
}
