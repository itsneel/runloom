# Compose application configuration

Compose mounts these files read-only into API, worker and local-admin. The containers retain `read_only: true`. Files use JSON-compatible YAML; the Go application accepts YAML, while the local setup safety checker requires this JSON-compatible form. No credentials belong in these files.

## Selecting files

| Compose variable | Default file | Used by |
| --- | --- | --- |
| `RUNLOOM_BASE_CONFIG_PATH` | `compose/config/base.yaml` (PostgreSQL) | API, worker, local-admin |
| `RUNLOOM_API_CONFIG_PATH` | `compose/config/api.yaml` (localhost:4200) | API |
| `RUNLOOM_WORKER_CONFIG_PATH` | `compose/config/worker.yaml` | Worker |
| `RUNLOOM_CONSUMER_CONFIG_PATH` | `compose/config/consumer.yaml` | API with consumer overlay |
| `RUNLOOM_VAULT_CONFIG_PATH` | `compose/config/vault.yaml` | API and worker with secrets overlay |
| `RUNLOOM_EMAIL_CONFIG_PATH` | Required with email overlay; copy `email.example.yaml` | Worker |
| `RUNLOOM_PRICING_CONFIG_PATH` | `compose/config/pricing.yaml` | Worker with pricing overlay |

`make local-up` selects `base.mongodb.yaml` and `api.local.yaml` using `deploy/local.env`. `make fresh-up` selects the same MongoDB file and `api.fresh.yaml`, using a separate project and ports. `make docker-up-mongo` selects the MongoDB base and database credential file automatically. Existing named volumes and secret files are preserved.

## Customization and migration

1. Copy the relevant configuration outside the repository, edit its settings, and set its selector above to an absolute path. Do not place passwords, tokens or encryption keys in configuration. Keep using the separate `RUNLOOM_DATABASE_SECRET_PATH`, `RUNLOOM_LLM_API_KEY_PATH`, `RUNLOOM_SMTP_PASSWORD_PATH`, `RUNLOOM_SECRETS_KEY_PATH` and `RUNLOOM_PROVISIONER_SECRET_PATH` mounts.
2. Move application-setting overrides from your deployment environment into the selected file. For example, database driver/name go in the base file; model, provider and LLM endpoint go in the worker file; Google shortcut and token issuer go in the API file. Environment variables inside file contents are not expanded. Helm and native configuration behavior are unchanged.
3. If changing `UI_PORT` or `RUNLOOM_PUBLIC_URL`, also set `RUNLOOM_OIDC_ISSUER` in the API file to the same origin plus `/auth/realms/runloom`. The bundled API files cover localhost ports 4200, 14200 and 24200. Existing Keycloak realms may also need their redirect origins updated explicitly.
4. Keep configuration files readable by container UID 65532 (non-secret files can use mode 0644). Keep actual credential files separately protected. Paths supplied to Compose are relative to the repository root when using the documented `--project-directory` flag.
5. Recreate the affected services after changes; configuration is loaded only at startup. For the local stack rerun `make local-up`. For a manually managed stack use the same project, environment and overlays with `up -d --force-recreate api worker`. Do not use `down -v`.

The application receives the same `/etc/runloom/config.yaml` and `/etc/runloom/config.d/*.yaml` paths as before. Changing the mount mechanism does not change workflow or API behavior.
