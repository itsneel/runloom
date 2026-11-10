# Independent backend services

Updated: 2026-10-15. Audience: developers and operators. The original service split uses a fresh installation, not an existing-data migration. Assistant provisioning adds its database without replacing existing service data.

## 1. Ownership

Optional per-service logging sidecars and Prometheus are configured in [observability](observability.md). Each service consumes the independent Go observability SDK as a pinned vendor release; normal builds do not access sibling source.

Keycloak remains the identity provider. Login, passwords, Google federation, recovery and token issuance stay there. Runloom Auth owns application access, not user passwords.

| Process | Owned records | Dependencies |
| --- | --- | --- |
| Auth (`server/auth/cmd/auth`) | Team-access bindings, platform roles/settings, service accounts and audit; Keycloak-backed organization administration | Keycloak organizations/membership; published workflow references and vault audit metadata from Workflows |
| Workflows (`server/workflows/cmd/workflows`) | Definitions, versions, commits, projects, edit leases, runs, execution audit, encrypted secrets and consumer admission/idempotency | Auth permissions; Management prompts, policies and usage; Temporal |
| Management (`server/management/cmd/management`) | Prompt versions, budget policies, policy changes and LLM usage ledger | Auth permissions; read-only workflow/project/run references |
| Worker (`server/workflows/cmd/worker`) | Execution observations and audit in the Workflow database | Temporal; Management accounting API |
| Assistant (`server/assistant/cmd/assistant`) | Private conversations, messages and planning jobs | Verified human identity; Auth membership API; configured model provider |
| Gateway (`server/gateway/cmd/gateway`) | None | Public API routing to owner services |

Each owning service has a separate database and database login. The worker belongs to Workflows and shares only that database. Each service has its own Go module, source, tests, Makefile, vendored dependencies, Dockerfile and build context. No root Go module, Go workspace, sibling import or source replacement is used. Auth and Management do not depend on Temporal, QuickJS or the execution engine. The monolithic `cmd/api` and shared backend image have been removed. Small wire contracts and transport adapters are owned independently by each consumer; changing one does not rebuild another.

Cross-domain access uses the explicit HTTP adapters in [services](../server/workflows/internal/services). A service does not receive another owner's database credentials. Reusable storage adapters initialize some unused empty tables/collections; this does not authorize cross-domain writes. No cross-database joins, foreign keys or distributed transaction are assumed. Auth checks published workflow grants through Workflows; Management validates project/policy references through Workflows. Callers fail closed when a required dependency is unavailable.

## 2. Authentication and discovery

Workflows automatically provisions one Temporal namespace per organization and workers discover them without restarts. See [organization namespaces and searchable run metadata](temporal-organizations.md) for mounted configuration, permissions, environment isolation and fresh-run compatibility rules. Auth and Management do not provision or connect to Temporal.

The independent [Go token-verification SDK](../sdk/go-auth/README.md) checks signatures, issuer, audience, expiry, not-before, access-token type and human/machine claims. Token verification is not authorization: destination services still check team membership, workflow grants, scopes, account status and credential generation on every relevant request.

Compose discovery uses `auth:8080`, `workflows:8080` and `management:8080`. Helm uses release-prefixed Kubernetes Service names. Studio and consumer paths remain `/api/v1/...`; Angular calls one public gateway, not individual processes.

The gateway mounts no database, workflow-encryption or provisioning credentials. It blocks `/internal/` and strips caller-supplied internal identity/actor headers. Destination services reject public routes owned by another service.

Internal `/internal/v1/` calls use independent workload credentials and operation allowlists. Each process receives only its own token. The trust ConfigMap contains SHA-256 hashes of those tokens, not the raw tokens. Auth re-verifies the human JWT when binding a user; a caller cannot assert a trusted user ID. Secret values/ciphertexts are excluded from identity projections. Only the worker can write usage through the Management internal API.

Local service calls use HTTP on the Compose network. Hosted use requires a private network and encrypted transport, such as a TLS-enabled service mesh. Automated workload-token rotation and application-managed inbound TLS are not implemented. Do not expose internal service ports publicly.

## 3. Fresh local setup

Prerequisites: local Docker Engine/Desktop, Docker Compose with `!override` support, Make and Node.js. Run commands from the repository root. The script rejects remote Docker endpoints.

```sh
make local-services-up SERVICES_DATABASE=mongodb
# Or, a separate fresh PostgreSQL installation:
make local-services-up SERVICES_DATABASE=postgres
```

| Installation | Studio | Gateway | Temporal UI | Compose project |
| --- | --- | --- | --- | --- |
| MongoDB | http://localhost:34200/ | http://localhost:38080/ | http://localhost:38233/ | `runloom-services-mongodb` |
| PostgreSQL | http://localhost:35200/ | http://localhost:39080/ | http://localhost:39234/ | `runloom-services-postgres` |

The selection chooses application storage. Keycloak continues using its own PostgreSQL database in both modes. Each installation has an independent Keycloak realm, Temporal instance, volumes and local credentials. Only the selected application database engine starts. Switching this argument selects another installation; it does not convert data.

Startup builds the four owner-service images, the separate Workflows worker, the stateless gateway, bootstrap tools, all four Studio Angular apps and the independent documentation image. It starts dependencies, provisions owner-restricted databases, verifies denied cross-database access, ensures local accounts and provisions the Keycloak service-account client. MongoDB uses an authenticated replica set. PostgreSQL database access is revoked from `PUBLIC` for each service database. Existing owner credentials are reused; setup never silently resets a password or deletes data. Documentation runs at a separate origin: port 34400 for MongoDB or 35400 for PostgreSQL.

Assistant general chat can build new main workflow drafts. Workflow-scoped chat can fix/enhance the selected workflow or create child attachments through the workflow owner's API. Updated Studio requests automatic draft saving with current human credentials; older jobs retain explicit review. It uses the current human bearer for context and approval, never a privileged internal service credential or another service's database. Its model configuration, separate database Secret and limits are documented in [Assistant](assistant.md). Hosted installations with `assistant.enabled=true` must also provision `runloom_assistant` and `assistant.databaseSecret`; the existing three-service prerequisites below do not cover that additional resource.

Public local-only logins: `admin@runloom.test` / `RunloomLocal!123` (Runloom owner, General admin) and `member@runloom.test` / `RunloomMember!123` (Runloom member, General editor). Both are automatically ensured by local setup. Existing passwords and existing member roles are preserved. Never enable this local bootstrap in hosted environments.

Runtime files live under `~/.config/runloom/local/services/<compose-project>/`. Configuration is mounted YAML; database URLs, service tokens, the provisioner secret and the raw 32-byte workflow encryption key are separate private files. Back up that directory separately from databases, especially the workflow key. Losing or regenerating the key makes saved secrets unreadable.

```sh
# Prepare private configuration and validate Compose without starting containers:
make local-services-config SERVICES_DATABASE=mongodb

# Stop this installation, preserving its volumes and credential files:
make local-services-down SERVICES_DATABASE=mongodb

# Re-running up safely reuses this installation and its credentials:
make local-services-up SERVICES_DATABASE=mongodb
```

These commands do not stop or alter an older `runloom-local`, `runloom-fresh`, Kubernetes installation or unrelated Docker project. They do not import old workflows. Staging deployment is a separate process.

## 4. Helm and externally supplied databases

The chart requires `splitServices.enabled=true` (the default) to deploy Auth, Workflows and Management alongside the gateway and worker. The chart accepts either `database.driver=mongodb` or `database.driver=postgres`. `make k8s-up` automatically prepares a local database server and credentials, then enables the chart's `databaseSetup` pre-install/pre-upgrade Job to provision owner-restricted databases before deploying servers. MongoDB is the default and `K8S_DATABASE=postgres` selects a fresh PostgreSQL setup. Direct Helm/hosted deployments can enable the same Job with existing bootstrap and owner URL Secrets, or provision the databases independently. Hosted installations use `development.enabled=false` and must also supply external Keycloak and Temporal settings. See the [chart prerequisites and upgrade notes](../charts/runloom/README.md).

Supply `splitServices.databases.<owner>.name`, `.existingSecret` and `.secretKey` for `auth`, `workflows` and `management`. Each Secret contains its complete connection URL, including database name, authentication source and TLS options as appropriate. The worker mounts the Workflow database Secret. Database URLs must match their configured database names; PostgreSQL selects its database through the URL.

Supply `splitServices.credentials.auth`, `.workflows`, `.management` and `.worker`: four Secret names, each with a `token` key containing a distinct random credential of at least 32 characters. `splitServices.trustConfigMap` identifies a ConfigMap with a `trust.json` key mapping these four workload names to their token SHA-256 hashes. Secret-manager controllers may create these Secrets; no secret values belong in Helm values or ConfigMaps.

The Keycloak provisioner secret mounts only into Auth. The existing workflow-vault configuration mounts the encryption key only into Workflows and the worker. Existing LLM, email, ingress and independent UI configuration still applies. Application settings mount as ConfigMaps, with credential paths pointing to Secret volumes.

Compose and Kubernetes are separate installations. The local kind helper provisions databases, service credentials, the Keycloak provisioner and the local administrator/organization as part of startup. Database bootstrap, identity provisioning, TLS/network policy and backups remain operator responsibilities for a hosted installation. Do not point a fresh split deployment at monolith databases.

Image values include `images.auth`, `images.workflows`, `images.worker`, `images.management`, `images.assistant` and `images.gateway`. The worker uses its own runtime image, not the Workflow API image. Chart 0.2.0 rejects the old `images.backend` and shared `database.name/existingSecret/secretKey` settings. Per-service scaling overrides live under `splitServices.auth/workflows/management`. Helm values contain references, not credential values. The local Make helper generates these resources before Helm installation and verifies/reuses them on upgrades. `K8S_SERVICE_VALUES=/absolute/path/to/service-values.yaml` is optional and opts into operator-managed resources. Split-service upgrades preserve current settings while dropping obsolete defaults; monolith upgrades require a separate fresh installation or an explicit migration project.

Workflow runtime packaging uses two independently buildable Dockerfiles in the same owner project. `make -C server/workflows docker-build` builds both; `docker-build-api IMAGE=...` and `docker-build-worker WORKER_IMAGE=...` build either alone. Root equivalents are `make docker-build-workflows-api` and `make docker-build-worker WORKER_IMAGE=...`. Compose uses `WORKFLOWS_IMAGE` for the API and `WORKER_IMAGE` for the worker; `make local-services-up` builds both automatically. `make k8s-upgrade` builds/loads both and sets their independent Helm values. Direct Helm upgrades must supply `images.worker.repository/tag/pullPolicy` as well as `images.workflows`; an old combined image tag must not be assumed to exist in the worker repository. No database migration is required.

Both workflow runtime images retain the JavaScript sandbox. Only the API image contains `runloom-workflows`; only the worker image contains `runloom-worker`. HTTP checks use each executable's early `healthcheck <URL>` subcommand rather than a standalone probe binary. They keep the same non-root, shell-free runtime and workflow-owned database boundary. Go build contexts exclude `_test.go`, `tests`, `__tests__` and `testdata`; Studio excludes test sources except its build-time boundary checker. Runtime images contain no test sources or build tools. Splitting images reduces each deployment's payload, but shared layers mean their sizes must not be added to estimate unique node storage.

Auth packaging likewise separates the server from local account setup. `server/auth/Dockerfile` contains only the Auth executable, including its health-check subcommand; `Dockerfile.local-admin` contains only the provisioning executable. `make -C server/auth docker-build` builds both independently. Root commands `make docker-build-auth-api` and `make docker-build-local-admin LOCAL_ADMIN_IMAGE=...` build either alone. Compose's `local-admin` setup service uses `LOCAL_ADMIN_IMAGE`, never `AUTH_IMAGE`. The local services helper builds both and automatically provisions accounts before reporting startup success.

Helm's `images.localAdmin.repository/tag/pullPolicy` records the setup image independently of `images.auth`. The guarded Kind helper builds/loads it and runs a short-lived Job after identity services are ready. `k8s-open` reads this configured image; it no longer discovers or executes a provisioning command in the Auth runtime. Upgrade older releases with `make k8s-upgrade` before opening them, or explicitly supply all three `images.localAdmin` fields with a built/loaded image on direct Helm upgrades. Merely setting these image values does not enable default accounts in hosted Helm installations.

No database migration, credential rotation or password reset is required. Repeated local setup preserves existing user identities, passwords, organization/team data and member roles. `make local-admin-reset` remains the explicit password-reset operation. After each database stack is running, use `make test-admin-setup-live SERVICES_DATABASE=mongodb` or `SERVICES_DATABASE=postgres` to verify browser login before/after repeated setup. With Kind Studio forwarding active, use `RUNLOOM_ADMIN_TEST_KIND=1 make test-admin-setup-live`. Tests compare identities, roles and workflow catalog IDs/versions without logging user content; non-default passwords can be supplied through `RUNLOOM_TEST_PASSWORD` and `RUNLOOM_TEST_MEMBER_PASSWORD`.

## 5. Independent builds

Run `make build`, `make test`, `make race`, `make vet` or `make docker-build` inside any of `server/auth`, `server/workflows`, `server/management`, `server/gateway` and `tools/database-setup`. Binaries go into that project's `bin/` directory. Copying any one of those directories to another repository preserves its build. Root `make build-server` and `make test-server` only orchestrate the independent projects.

The separate SDK is pinned to `v0.1.0` and vendored in each consuming service. It is not published remotely by this change. Dependency updates are explicit maintenance operations: update the SDK version and regenerate each consumer's vendor snapshot, reviewing the diff and its tests. Normal builds do not read `sdk/` or regenerate dependencies. Preserve all vendored upstream license files. Repository-wide contract tests may compare projects, but are not required to compile a service.

## 6. Verification

```sh
make test-auth-sdk
make test-server
make check-service-boundaries
```

The SDK tests cover signed human and machine tokens, malformed headers and invalid claims. Service tests exercise actual HTTP boundaries with isolated stores, operation denial, prompt ownership, usage idempotency and dependency failures. Database setup checks successful owner authentication and failed cross-database access on every startup.

The live [service smoke](../scripts/services-smoke.mjs) uses browser login and exercises the gateway, secrets, management reads, publishing, Temporal activity/timer/callback/signal/pause/resume, execution audit, service-account provisioning, consumer idempotency/cancel and revocation. It creates uniquely named synthetic records and revokes its temporary service account. It never prints or persists bearer tokens or client secrets.

```sh
RUNLOOM_UI_URL=http://localhost:34200 \
RUNLOOM_PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
node scripts/services-smoke.mjs
```

Use port `35200` to verify PostgreSQL. Set `RUNLOOM_CHROME` if using a system Chrome executable instead of Playwright's installed browser. These tests do not send real email or incur external LLM costs. A successful health check alone is not end-to-end verification.

`make test-services-live SERVICES_DATABASE=mongodb` (or `postgres`) runs the browser smoke and the [live service LLM test](../server/workflows/internal/services/tests/llm_live_test.go). Supply the same Playwright/Chrome environment settings. The latter starts a temporary worker on a unique queue, connects only to the Workflows database, and calls the separately deployed Management service over its authenticated HTTP API. Policy edits go through the authenticated public Management API; the test never opens Management's database. The provider is a local fake. Management is loopback-accessible on port 38083 (MongoDB) or 39083 (PostgreSQL) for this verification; internal operations still require workload credentials. It verifies parent/child retries, Management-only usage, deterministic replay, hard-limit pause and resume without duplicate billing. Temporary workers are stopped after the test; synthetic records remain for inspection.

Verified locally on 2026-10-11: fresh MongoDB and PostgreSQL startup, repeated startup with retained credentials, database isolation checks, all three Angular Docker builds, browser/API smoke, live LLM accounting and budget/replay checks, SDK tests and affected Go/Node tests. Helm rendering is checked separately; no Kubernetes or staging deployment was performed. External Google federation, paid providers and external email delivery were not exercised.
