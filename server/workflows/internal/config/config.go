package config

import (
	"strconv"
)

type Settings struct {
	TemporalAddress         string
	TemporalNamespace       string
	TemporalNamespacePrefix string
	TemporalRetentionDays   int
	TaskQueue               string
	DataDir                 string
	APIAddress              string
	DatabaseDriver          string
	DatabaseURL             string
	DatabaseName            string
}

// FromEnv retains its native-call-site name for compatibility; after Initialize
// selects a mounted file, every setting comes from that configuration snapshot.
func FromEnv() Settings {
	return Settings{
		TemporalAddress:         env("TEMPORAL_ADDRESS", "localhost:7233"),
		TemporalNamespace:       env("TEMPORAL_NAMESPACE", "default"),
		TemporalNamespacePrefix: env("TEMPORAL_NAMESPACE_PREFIX", "runloom-local"),
		TemporalRetentionDays:   retentionDays(),
		TaskQueue:               env("RUNLOOM_TASK_QUEUE", "runloom"),
		DataDir:                 env("RUNLOOM_DATA_DIR", "var/runloom/definitions"),
		APIAddress:              env("RUNLOOM_API_ADDRESS", "127.0.0.1:"+env("RUNLOOM_API_PORT", strconv.Itoa(8080))),
		DatabaseDriver:          env("RUNLOOM_DATABASE_DRIVER", "file"),
		DatabaseURL:             Value("RUNLOOM_DATABASE_URL"),
		DatabaseName:            env("RUNLOOM_DATABASE_NAME", "runloom"),
	}
}

func retentionDays() int {
	v, err := strconv.Atoi(env("TEMPORAL_RETENTION_DAYS", "30"))
	if err != nil {
		return 0
	}
	return v
}

func env(name, fallback string) string {
	if value := Value(name); value != "" {
		return value
	}
	return fallback
}
