//go:build repository_integration

package config

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// These are read-only render checks, never cluster operations or Docker starts.
func TestHelmMountedDocuments(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	for _, production := range []bool{false, true} {
		args := []string{"template", "config-test", "charts/runloom", "--show-only", "templates/configmap.yaml", "--set", "consumer.provisioning.enabled=true", "--set", "consumer.provisioning.adminURL=http://keycloak:8080/auth", "--set", "consumer.provisioning.existingSecret=provisioner", "--set", "llm.existingSecret=llm-key", "--set", "llm.pricingConfigMap=prices", "--set", "email.enabled=true", "--set", "email.host=smtp.example.test", "--set", "email.username=sender", "--set", "email.existingSecret=smtp"}
		if production {
			args = append(args, "-f", "charts/runloom/values-production.yaml")
		}
		cmd := exec.Command("helm", args...)
		cmd.Dir = "../../../.."
		output, err := cmd.Output()
		if err != nil {
			t.Fatal("Helm config rendering failed; output omitted")
		}
		decoder := yaml.NewDecoder(bytes.NewReader(output))
		found := 0
		for {
			var document struct {
				Data map[string]string `yaml:"data"`
			}
			if err := decoder.Decode(&document); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal("invalid chart YAML")
			}
			for _, component := range []string{"api", "worker"} {
				content, ok := document.Data[component+".yaml"]
				if !ok {
					continue
				}
				found++
				verifyDeploymentDocument(t, []string{content})
				if component == "api" && Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE") == "" {
					t.Fatal("API provisioning setting missing")
				}
				if component == "worker" && (Value("RUNLOOM_SMTP_PASSWORD_FILE") == "" || Value("RUNLOOM_LLM_PRICING_FILE") == "") {
					t.Fatal("worker feature settings missing")
				}
			}
		}
		if found != 2 {
			t.Fatal("expected separate API and worker YAML documents")
		}
	}
}

func TestComposeMountedDocuments(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	for _, profile := range []string{"local", "fresh"} {
		args := []string{"compose", "--project-directory", ".", "--env-file", "deploy/" + profile + ".env", "-f", "compose/compose.yaml", "-f", "compose/compose.local.yaml", "-f", "compose/compose.secrets.yaml", "-f", "compose/compose.consumer.yaml", "-f", "compose/compose.email.yaml", "-f", "compose/compose.pricing.yaml", "--profile", "local-setup", "config", "--format", "json"}
		cmd := exec.Command("docker", args...)
		cmd.Dir = "../../../.."
		cmd.Env = append(os.Environ(), "RUNLOOM_SECRETS_KEY_PATH=/private/test-key", "RUNLOOM_SECRETS_KEY_GID=1234", "RUNLOOM_PROVISIONER_SECRET_GID=2345", "RUNLOOM_PROVISIONER_SECRET_PATH=/private/test-provisioner", "RUNLOOM_EMAIL_CONFIG_PATH=./compose/config/email.example.yaml", "RUNLOOM_SMTP_PASSWORD_PATH=/private/test-smtp", "RUNLOOM_LLM_PRICING_PATH=/private/test-prices")
		output, err := cmd.Output()
		if err != nil {
			t.Fatal("Compose rendering failed; output omitted")
		}
		var model struct {
			Configs  map[string]struct{ File string }
			Services map[string]struct {
				Environment map[string]string
				Configs     []struct{ Source, Target string }
			}
		}
		if err := json.Unmarshal(output, &model); err != nil {
			t.Fatal("invalid Compose result")
		}
		for _, name := range []string{"api", "worker", "local-admin"} {
			service := model.Services[name]
			if service.Environment["RUNLOOM_CONFIG_FILE"] != "/etc/runloom/config.yaml" {
				t.Fatal("missing file selector")
			}
			if name != "local-admin" && len(service.Environment) != 1 {
				t.Fatal("backend still has environment configuration")
			}
			var files []string
			for _, mount := range service.Configs {
				content, err := os.ReadFile(model.Configs[mount.Source].File)
				if err != nil {
					t.Fatal("mounted Compose configuration file is unreadable")
				}
				files = append(files, string(content))
			}
			if len(files) == 0 {
				t.Fatal("configuration not mounted")
			}
			verifyDeploymentDocument(t, files)
			if FromEnv().DatabaseDriver != "mongodb" {
				t.Fatal("wrong local database configuration")
			}
			if name == "worker" && Value("RUNLOOM_SMTP_HOST") != "smtp.example.test" {
				t.Fatal("email overlay ignored")
			}
			if name == "api" && Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE") == "" {
				t.Fatal("provisioning overlay ignored")
			}
		}
	}
}

func verifyDeploymentDocument(t *testing.T, documents []string) {
	t.Helper()
	values := map[string]string{}
	for _, document := range documents {
		var part map[string]string
		if err := yaml.Unmarshal([]byte(document), &part); err != nil {
			t.Fatal("invalid mounted YAML; contents omitted")
		}
		for key, value := range part {
			values[key] = value
		}
	}
	dir := t.TempDir()
	credential := filepath.Join(dir, "db")
	if err := os.WriteFile(credential, []byte("synthetic-test-connection"), 0600); err != nil {
		t.Fatal(err)
	}
	if values["RUNLOOM_DATABASE_URL_FILE"] == "" {
		t.Fatal("database must reference a credential file")
	}
	values["RUNLOOM_DATABASE_URL_FILE"] = credential
	data, err := yaml.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	fixture(t, string(data))
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
}
