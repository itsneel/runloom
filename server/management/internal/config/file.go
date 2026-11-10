package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	yaml "gopkg.in/yaml.v3"
)

// A startup snapshot: configuration changes require a restart, not a mid-run reload.
var mounted atomic.Pointer[map[string]string]

var allowed = strings.Fields(`TEMPORAL_ADDRESS TEMPORAL_NAMESPACE RUNLOOM_TASK_QUEUE
RUNLOOM_TELEMETRY_ENDPOINT RUNLOOM_DATA_DIR RUNLOOM_API_ADDRESS RUNLOOM_API_PORT RUNLOOM_DATABASE_DRIVER
RUNLOOM_DATABASE_NAME RUNLOOM_DATABASE_URL_FILE RUNLOOM_WORKER_HEALTH_ADDRESS
RUNLOOM_OIDC_URL RUNLOOM_OIDC_ISSUER RUNLOOM_OIDC_JWKS_URL RUNLOOM_OIDC_CLIENT_ID
RUNLOOM_OIDC_REALM RUNLOOM_GOOGLE_LOGIN_ENABLED RUNLOOM_EXECUTION_AUDIENCE
RUNLOOM_OIDC_REQUIRED_ACR RUNLOOM_OIDC_MAX_AUTH_AGE RUNLOOM_SENSITIVE_RUN_ROLES
RUNLOOM_KEYCLOAK_ADMIN_URL RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID
RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE RUNLOOM_SECRETS_KEY_FILE
RUNLOOM_LLM_BASE_URL RUNLOOM_LLM_DEFAULT_MODEL RUNLOOM_LLM_PROVIDER
RUNLOOM_LLM_API_KEY_FILE RUNLOOM_LLM_PRICING_FILE RUNLOOM_SMTP_HOST
RUNLOOM_SMTP_PORT RUNLOOM_SMTP_FROM RUNLOOM_SMTP_TLS RUNLOOM_SMTP_USERNAME
RUNLOOM_SMTP_PASSWORD_FILE RUNLOOM_SMTP_ALLOWED_DOMAINS RUNLOOM_CODE_RUNNER
RUNLOOM_AUTH_URL RUNLOOM_WORKFLOWS_URL RUNLOOM_MANAGEMENT_URL
RUNLOOM_SERVICE_TOKEN_FILE RUNLOOM_SERVICE_TRUST_FILE`)

// Initialize loads RUNLOOM_CONFIG_FILE and sorted config.d/*.yaml fragments beside
// it. Mounted mode does not inherit ambient environment settings or secret values.
// Native tools without a config file retain explicit environment compatibility.
func Initialize() error {
	path := os.Getenv("RUNLOOM_CONFIG_FILE")
	if path == "" {
		mounted.Store(nil)
		return nil
	}
	values := map[string]string{}
	if err := readConfig(path, values); err != nil {
		return err
	}
	fragments, err := filepath.Glob(filepath.Join(filepath.Dir(path), "config.d", "*.yaml"))
	if err != nil {
		return errors.New("invalid configuration directory")
	}
	for _, fragment := range fragments {
		if err := readConfig(fragment, values); err != nil {
			return err
		}
	}
	if file := values["RUNLOOM_DATABASE_URL_FILE"]; file != "" {
		secret, err := os.ReadFile(file)
		if err != nil || len(secret) > 65536 || len(bytes.TrimSpace(secret)) == 0 {
			return errors.New("cannot read database credential file; content omitted")
		}
		values["RUNLOOM_DATABASE_URL"] = strings.TrimSpace(string(secret))
	}
	if driver := values["RUNLOOM_DATABASE_DRIVER"]; driver != "" && driver != "file" && values["RUNLOOM_DATABASE_URL"] == "" {
		return errors.New("database configuration requires RUNLOOM_DATABASE_URL_FILE")
	}
	mounted.Store(&values)
	return nil
}

func readConfig(path string, values map[string]string) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open mounted application configuration")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1048577))
	if err != nil || len(data) > 1048576 {
		return errors.New("cannot read application configuration or file exceeds 1 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var entries map[string]string
	if err := decoder.Decode(&entries); err != nil || len(entries) == 0 {
		return errors.New("invalid application YAML configuration; content omitted")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("application configuration must contain one YAML document")
	}
	for key, value := range entries {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return errors.New("unknown or secret-valued configuration setting; use supported keys and credential file paths")
		}
		values[key] = value
	}
	return nil
}

func Value(name string) string {
	if values := mounted.Load(); values != nil {
		return (*values)[name]
	}
	return os.Getenv(name)
}
