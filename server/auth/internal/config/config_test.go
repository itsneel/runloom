package config

import "testing"

func TestRunloomDefaults(t *testing.T) {
	for _, name := range []string{"RUNLOOM_TASK_QUEUE", "RUNLOOM_DATA_DIR", "RUNLOOM_DATABASE_NAME", "RUNLOOM_DATABASE_DRIVER", "RUNLOOM_DATABASE_URL", "RUNLOOM_API_ADDRESS", "RUNLOOM_API_PORT"} {
		t.Setenv(name, "")
	}
	got := FromEnv()
	if got.TaskQueue != "runloom" || got.DatabaseName != "runloom" || got.DataDir != "var/runloom/definitions" || got.APIAddress != "127.0.0.1:8080" || got.DatabaseDriver != "file" {
		t.Fatal("unexpected Runloom defaults")
	}
}

func TestRunloomOverrides(t *testing.T) {
	t.Setenv("RUNLOOM_TASK_QUEUE", "runloom-test")
	t.Setenv("RUNLOOM_DATA_DIR", t.TempDir())
	t.Setenv("RUNLOOM_DATABASE_NAME", "runloom_test")
	t.Setenv("RUNLOOM_DATABASE_DRIVER", "mongodb")
	t.Setenv("RUNLOOM_DATABASE_URL", "mongodb://localhost:27017/runloom_test")
	t.Setenv("RUNLOOM_API_ADDRESS", "127.0.0.1:18080")
	got := FromEnv()
	if got.TaskQueue != "runloom-test" || got.DatabaseName != "runloom_test" || got.DatabaseDriver != "mongodb" || got.APIAddress != "127.0.0.1:18080" || got.DatabaseURL == "" {
		t.Fatal("Runloom environment overrides were not applied")
	}
}
