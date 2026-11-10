package tests

import (
	"github.com/example/runloom/assistant/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMountedConfigurationAndSecretFiles(t *testing.T) {
	dir := t.TempDir()
	uri := filepath.Join(dir, "database-url")
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(uri, []byte("postgres://synthetic-file-only"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNLOOM_CONFIG_FILE", file)
	t.Setenv("RUNLOOM_ASSISTANT_MODEL", "ambient-must-not-leak")
	defer func() { _ = os.Unsetenv("RUNLOOM_CONFIG_FILE"); _ = config.Initialize() }()
	if err := os.WriteFile(file, []byte("RUNLOOM_DATABASE_DRIVER: postgres\nRUNLOOM_DATABASE_URL_FILE: "+uri+"\nRUNLOOM_ASSISTANT_PROVIDER: disabled\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Initialize(); err != nil {
		t.Fatal(err)
	}
	if config.Value("RUNLOOM_DATABASE_URL") != "postgres://synthetic-file-only" || config.Value("RUNLOOM_ASSISTANT_MODEL") != "" {
		t.Fatal("mounted mode inherited ambient settings")
	}
	_ = os.WriteFile(file, []byte("RUNLOOM_ASSISTANT_API_KEY: do-not-echo-this\n"), 0600)
	if err := config.Initialize(); err == nil || strings.Contains(err.Error(), "do-not-echo-this") {
		t.Fatal("inline secret key accepted or leaked")
	}
}
