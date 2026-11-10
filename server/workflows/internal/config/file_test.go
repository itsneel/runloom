package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNLOOM_CONFIG_FILE", path)
	t.Cleanup(func() { mounted.Store(nil) })
	return path
}

func TestMountedConfigAndOrderedFragments(t *testing.T) {
	path := fixture(t, "TEMPORAL_ADDRESS: temporal:7233\nRUNLOOM_DATABASE_DRIVER: file\nRUNLOOM_SMTP_PORT: 1025\nRUNLOOM_GOOGLE_LOGIN_ENABLED: false\n")
	dir := filepath.Join(filepath.Dir(path), "config.d")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"10-worker.yaml": `{"RUNLOOM_LLM_DEFAULT_MODEL":"first"}`, "20-override.yaml": "RUNLOOM_LLM_DEFAULT_MODEL: second\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TEMPORAL_ADDRESS", "wrong:7233")
	t.Setenv("RUNLOOM_LLM_API_KEY", "ambient-secret")
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
	if FromEnv().TemporalAddress != "temporal:7233" || Value("RUNLOOM_LLM_DEFAULT_MODEL") != "second" || Value("RUNLOOM_SMTP_PORT") != "1025" || Value("RUNLOOM_LLM_API_KEY") != "" {
		t.Fatal("mounted configuration or precedence mismatch")
	}
}

func TestDatabaseCredentialFile(t *testing.T) {
	path := fixture(t, "RUNLOOM_DATABASE_DRIVER: postgres\n")
	if Initialize() == nil {
		t.Fatal("accepted database without credential file")
	}
	secret := filepath.Join(filepath.Dir(path), "db-url")
	if err := os.WriteFile(secret, []byte("postgresql://user:synthetic@postgres/db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("RUNLOOM_DATABASE_DRIVER: postgres\nRUNLOOM_DATABASE_URL_FILE: "+secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(FromEnv().DatabaseURL, "/db") {
		t.Fatal("credential file not loaded")
	}
}

func TestRejectInvalidAndSecretValuedConfig(t *testing.T) {
	for _, content := range []string{"", "[broken", "UNKNOWN: synthetic-secret\n", "RUNLOOM_LLM_API_KEY: synthetic-secret\n", "RUNLOOM_DATABASE_URL: synthetic-secret\n", "RUNLOOM_SMTP_PORT: 25\nRUNLOOM_SMTP_PORT: 26\n", "RUNLOOM_TASK_QUEUE: one\n---\nRUNLOOM_TASK_QUEUE: two\n", "RUNLOOM_TASK_QUEUE: {nested: synthetic-secret}"} {
		t.Run(content[:min(len(content), 20)], func(t *testing.T) {
			fixture(t, content)
			err := Initialize()
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("invalid config accepted or content disclosed")
			}
		})
	}
}

func TestMissingMountedFileFailsAndNativeEnvironmentRemainsSupported(t *testing.T) {
	t.Setenv("RUNLOOM_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	if Initialize() == nil {
		t.Fatal("missing mounted file accepted")
	}
	t.Setenv("RUNLOOM_CONFIG_FILE", "")
	t.Setenv("RUNLOOM_TASK_QUEUE", "native")
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
	if FromEnv().TaskQueue != "native" {
		t.Fatal("native compatibility failed")
	}
}
