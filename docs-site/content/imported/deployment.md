# Deployment

The independent-service installation is documented in [service deployment](service-split.md). Auth, Workflows and Management own separate databases; enabling Assistant adds a fourth owner and database. Each service has an independent image, with a stateless gateway and a separate Workflows worker. The shared backend image and monolithic API command have been removed. Local Compose setup is `make local-services-up SERVICES_DATABASE=mongodb` or `postgres`. Local Kubernetes setup is `make k8s-up`, which provisions its service databases, credentials and local accounts automatically; no override file is required. See [current Helm/local kind instructions](../charts/runloom/README.md). Direct Helm/hosted deployments require operator-provided database and workload credentials, and either provisioned databases or the configured database-setup Job.

The worker supports [Email SMTP configuration](email-node.md#worker-configuration) and [versioned LLM pricing](llm-costs.md#configure-versioned-pricing). Optional `compose/compose.email.yaml` and `compose/compose.pricing.yaml` overlays mount credentials/catalogues read-only; equivalent Helm values are documented in those guides. Base development deployments deliver Email nodes only to local Mailpit. Hosted Email is disabled in the production values example until configured.

JavaScript Custom Code uses the `runloom-code` binary bundled in the server image; no extra service or Node runtime is required. Upgrade both API and worker images before publishing code-node workflows. See [Custom Code deployment and limits](custom-code.md#build-and-deploy).

External API nodes need no additional service. Hosted deployments enable encrypted team credentials by provisioning one persistent 32-byte key and mounting it into API and worker using `compose/compose.secrets.yaml` or `workflowSecrets.existingSecret` in Helm. Guarded local setup creates/reuses this key automatically. See [workflow secret setup and backup requirements](api-nodes-and-secrets.md#operator-setup). The key must not be replaced on ordinary redeployments.

Consumer execution is owned by Workflows; service-account provisioning is owned by Auth. Both are reached through the gateway. Local setup automatically provisions and verifies the dedicated Keycloak client and mounts its provisioning credential into Auth only. Hosted deployments retain explicit operator bootstrap and credential configuration; see the [consumer setup guide](consumer-api.md#docker-compose-and-helm). No setup command replaces an existing realm or silently rotates credentials.

Application images are built independently for Auth, Workflow API, worker, Management, Assistant and Gateway, plus four Studio images (`runloom-ui`, `runloom-workflows-ui`, `runloom-management-ui`, `runloom-platform-ui`) and the separate `runloom-docs` documentation image. Separate database-setup and local-admin Dockerfiles package bootstrap tools. Each Angular Docker context is only its own project directory. The Studio host at `/` reverse-proxies `/remotes/workflows/`, `/remotes/management/` and `/remotes/platform/` to separate asset services. `/federation.manifest.json` configures runtime entry locations. Each Studio app installs a compiled platform library release; no app build reads library or sibling sources. Documentation has its own build and separate public origin; it is not a Studio remote. All images run as non-root users. See [independent platform-library consumption](../studio/libraries/platform/README.md).

For an existing self-hosted installation, follow the [version upgrade guide](upgrading.md) before changing images or chart versions. It covers backups, configuration compatibility, active runs, rollout verification and rollback limits.

## Image hardening and verification

All application/tool Dockerfiles use two stages and digest-pinned base images. Go builds are offline against each service's vendored dependencies with a pinned compiler, disabled automatic toolchain downloads, stripped debug/symbol tables and no embedded checkout paths or VCS metadata. Their final Distroless images contain required executables, license notices and runtime trust data, not a shell or compiler. Auth contains only its server executable; local Compose/Kind setup uses the dedicated local-admin image containing only the administrator executable. Workflow API and worker images each retain their own executable and JavaScript sandbox. Each runtime executable includes an early health-check subcommand, not a separate probe binary. See [image setup and upgrade commands](service-split.md#4-helm-and-externally-supplied-databases) for independent builds and preservation rules.

Angular builds run as the unprivileged Node user with dependency lifecycle scripts disabled. Production compilation runs without network access. Final images use Nginx Alpine Slim, contain compiled browser assets rather than Node or `node_modules`, omit source maps and suppress the Nginx version header. Nginx configuration is root-owned and read-only. The UI images still contain Alpine's basic shell/tools; they are not shellless.

Build contexts use explicit allowlists and trailing exclusions for credential files. Do not add private keys, credentials, production configuration or tokens to image layers or build arguments. Runtime credentials remain externally mounted. Preserve non-root users, read-only root filesystems, dropped capabilities, `no-new-privileges` and bounded writable `/tmp` mounts in deployment configuration; a Dockerfile alone cannot enforce every runtime restriction.

```bash
make check-docker-security
make docker-build
make test-docker-images
make scan-docker-images
```

Tests and scans default to `runloom-<application>:local`. To verify another consistently tagged set, pass `RUNLOOM_IMAGE_TEST_TAG=<tag>` in the environment. These checks create and remove only their own disposable containers/network; they do not redeploy the application or delete existing images/data. The scan exports images into a temporary directory, uses a digest-pinned Trivy container without mounting the Docker socket, downloads vulnerability databases and saves local JSON reports. It does not upload application images. Exported image archives are deleted after scanning; reports and the scanner database cache remain in the printed temporary directory. High/critical vulnerabilities or detected secrets make the scan command fail.

### Verification snapshot: 2026-10-20

All ten images built independently using tag `hardening-20261020`. All 21 Dockerfile/context checks passed. Smoke tests verified the three UI images under read-only/non-root restrictions, federation asset serving, SPA fallback, health-client execution and the actual workflow JavaScript sandbox. Go tests passed for all five services and database-setup with the pinned compiler. Database-dependent integration tests were not exercised; this is not full-stack or deployment verification.

Compared with existing local images tagged `dev-1792511679546-75d9a977`, `docker image ls` reported Studio images at approximately 31–32 MB instead of 93 MB, Workflows at 142 MB instead of 212 MB and Auth at 63 MB instead of 92 MB. These are local image-size observations against an earlier source build, not a controlled identical-source benchmark. Build caches and registry transfer sizes are different measurements. Do not remove required binaries, trust roots, embedded schemas or license notices simply to reduce size.

**Security scan is not clear.** No embedded secrets were detected, but the scan reported 85 high/critical occurrences representing 24 distinct advisories across Go binaries. Shared libraries appear in multiple executables/images, so occurrences are not distinct vulnerabilities. Required dependency remediation includes the following minimum fixed versions reported by the scanner; each upgrade needs refreshed independent vendor snapshots and compatibility/integration tests before release:

| Dependency | Reported fixed version |
| --- | --- |
| `github.com/jackc/pgx/v5` | `5.9.0` |
| `golang.org/x/crypto` | `0.55.0` |
| `golang.org/x/net` | `0.60.0` |
| `golang.org/x/text` | `0.41.0` |
| `google.golang.org/grpc` | `1.83.2` |

Each UI image also reported the medium-severity zlib advisory `CVE-2026-85091` (`1.3.2-r0`, fixed in `1.3.2-r1`); refresh the pinned runtime base when that package fix is incorporated. Dependency upgrades were not included in this packaging change. No findings were suppressed. Image scanning also does not fully assess vulnerabilities in bundled browser JavaScript or application authorization/business logic, and does not establish compliance.

Digest pins must be maintained: review upstream security releases, update all affected Dockerfiles, rebuild and rerun these checks before promotion. Pinning improves reproducibility but does not automatically install later patches. Follow [Docker's build guidance](https://docs.docker.com/build/building/best-practices/) and keep dependency vulnerability checks separate from Dockerfile structure checks.

## Docker Compose

Install Docker with a Compose version supporting `!override`, Make and a supported Node.js version for the local setup helper. From the repository root:

```bash
make docker-up
# Or choose the independent PostgreSQL installation:
make docker-up SERVICES_DATABASE=postgres
```

These commands use the independent-service setup helper. MongoDB is the default application database; only the selected application database engine starts. Keycloak always uses its own PostgreSQL database. Setup builds the services, worker, Studio apps, documentation and bootstrap images, provisions owner databases and local accounts, and waits for health checks. Temporal and Mailpit are development services. No LLM key is needed for startup. See [service setup and credentials](service-split.md#3-fresh-local-setup).

| Service | MongoDB installation | PostgreSQL installation |
| --- | --- | --- |
| Workflow studio | http://localhost:34200 | http://localhost:35200 |
| Gateway health | http://localhost:38080/api/v1/health | http://localhost:39080/api/v1/health |
| Documentation | http://localhost:34400 | http://localhost:35400 |
| Temporal UI | http://localhost:38233 | http://localhost:39234 |
| Identity administration | http://localhost:34200/auth/admin/ | http://localhost:35200/auth/admin/ |

Published ports bind to loopback. The UI forwards `/api/` to the gateway and `/auth/` to Keycloak on the internal Compose network. The UI container listens on 8080; it does not require privileged ports. Google credentials and production SMTP require separate operator configuration.

`make docker-up-mongo` explicitly selects MongoDB. Workflows API and worker share the Workflows owner's database; other services have separate databases and credentials. Switching `SERVICES_DATABASE` selects a separate installation, not a data migration. Repeat the selection for logs and shutdown.

Inspect and stop the stack with:

```bash
make docker-logs SERVICES_DATABASE=mongodb
make docker-down SERVICES_DATABASE=mongodb
```

Stopping retains the named database and Temporal volumes. Avoid `down --volumes` unless intentionally deleting development data. The Temporal server persists its SQLite file, but remains a development server.

All Compose files live in [compose/](../compose/). The helper combines the base file, the independent-service overlay and generated private installation configuration. Use the Make commands for complete setup; invoking the base file alone does not provision service databases or credentials. `make local-services-config SERVICES_DATABASE=mongodb` prepares and validates the installation configuration without starting containers. See [service setup](service-split.md#3-fresh-local-setup) for its private configuration directory. Bare `docker compose` no longer discovers a root file.

### Configuration and secrets

Compose runs without copying a `.env` file. It mounts application configuration at `/etc/runloom/config.yaml` plus component/feature fragments in `/etc/runloom/config.d/`. Ports and file paths may still be supplied through Compose interpolation or `deploy/local.env`; application settings now live in the selected YAML files, not deployment environment variables. See [Compose configuration files](../compose/config/README.md) for selectors and migration instructions. Database credentials are read from `/run/secrets/database-url`. Set `RUNLOOM_DATABASE_SECRET_PATH` to a private connection-string file when overriding the public local database defaults. Container connections must use reachable service hostnames, not native-host localhost addresses.

## Mounted application configuration

API and worker use the same YAML reader. `RUNLOOM_CONFIG_FILE` selects the mounted file; the application then reads sibling `config.d/*.yaml` fragments in lexical order, with later fragments overriding earlier keys. Files contain a flat mapping of supported setting names to scalar values. JSON is valid YAML and is used in the file-backed documents under `compose/config/` so local setup can validate them without extra parser dependencies. Compose mounts these with `configs.file`; no `configs.content` or writable container filesystem is required. File contents are literal: Compose does not interpolate variables inside them. Helm emits conventional YAML under the ConfigMap's `api.yaml` and `worker.yaml` keys, mounting only the relevant key read-only into each component. Helm's configuration checksum triggers a rollout when values change.

In mounted mode, ambient environment settings are ignored. Missing files, invalid YAML, duplicate keys, unknown names, inline secret-valued settings and missing database credential files fail startup. Database URLs, LLM keys, SMTP passwords, the workflow encryption key and provisioner secrets belong in separate Kubernetes/Compose Secrets. YAML contains only their file paths. Backend containers receive only `RUNLOOM_CONFIG_FILE` as a configuration environment variable. Third-party containers such as Keycloak and PostgreSQL retain their upstream configuration interfaces; local bootstrap safety flags remain explicit.

Settings are read once at startup, not hot-reloaded. Restart/recreate the application after changing mounted config or the database credential file. Deploy the matching backend image and chart together: an old image that does not understand mounted YAML cannot use the new chart. No live cluster is upgraded just by editing configuration in this repository.

For native development, copy [config.example.yaml](../config.example.yaml), configure private credential file paths, and export `RUNLOOM_CONFIG_FILE=/absolute/path/to/config.yaml` before running the API/worker. Without that selector, native environment-based configuration remains supported. The former `.env.example` template has been replaced, not loaded automatically.

The Compose database files in `deploy/local-postgres-url` and `deploy/local-mongodb-url` contain only public, local development defaults. Never replace their contents with real credentials. Supply external files using `RUNLOOM_DATABASE_SECRET_PATH` and `RUNLOOM_LLM_API_KEY_PATH`; ensure container UID 65532 can read them without exposing secrets publicly. The default empty LLM file intentionally supplies no provider credential.

The local service helper assigns fixed, distinct ports and Compose project names for MongoDB and PostgreSQL, as listed above. It does not use generic `UI_PORT` or `COMPOSE_PROJECT_NAME` overrides to select a different installation. Resolve occupied ports before startup, and use `SERVICES_DATABASE` consistently. For the smoke test, set `RUNLOOM_UI_URL` to that installation's Studio origin and supply `RUNLOOM_ACCESS_TOKEN_FILE` and `RUNLOOM_TEAM_ID`. Older monolith volumes are not automatically reused or migrated; read [the migration guide](rebranding.md) before an upgrade.

For LLM nodes, copy `compose/config/worker.yaml` to your own configuration file, set `RUNLOOM_LLM_BASE_URL` and `RUNLOOM_LLM_DEFAULT_MODEL` inside it, and select it with `RUNLOOM_WORKER_CONFIG_PATH`. Set `RUNLOOM_LLM_API_KEY_PATH=/absolute/private/llm-api-key` to a credential file readable by container UID 65532. Compose mounts that Secret only into the worker, and its YAML configuration references `/run/secrets/llm-api-key`. Do not put key values in shell history, ConfigMaps or committed files. The former raw `RUNLOOM_LLM_API_KEY` Compose setting is no longer used; native execution without mounted configuration retains its environment compatibility.

## Helm chart

Use the [Helm values guide](helm-values.md) to choose a deployment mode, prepare required resources, and configure optional features.

The chart at [charts/runloom](../charts/runloom) deploys the gateway, Auth, Workflows API, worker, Management and four independent Studio applications. Assistant and documentation have separate Deployments and Services when enabled. It also supplies per-component configuration, probes, resource requests/limits, optional autoscaling/disruption budgets and optional ingress. The worker's Service is internal, not a public execution API. Workflow-node LLM credentials mount into the worker; Assistant uses its own provider configuration. Documentation has a separate `docs.publicURL` and optional `docs.ingress`, not a Studio subpath.

The development chart includes a persistent Temporal development server and Keycloak's PostgreSQL database as StatefulSets, plus Keycloak and Mailpit Deployments. The local Kind helper separately prepares the selected application database server and credentials; the database-setup hook provisions service-owned databases. Direct Helm installations must supply those resources or use externally provisioned databases. The chart defaults to `database.driver=postgres`; `mongodb` is also supported. Set `auth.publicURL` to the exact Studio browser origin (default http://localhost:4200). The cluster needs a default StorageClass, or set `development.persistence.storageClass`.

Development Keycloak uses a 128–384 MiB Java heap and two JVM processors to leave room for native memory within its 1 GiB container limit. Override `development.keycloakJavaOptsHeap` and `development.keycloakJavaOptsAppend` for your local workload. These use Keycloak's [supported Java configuration](https://www.keycloak.org/server/containers#_specifying_different_memory_settings). If the Kind node itself is OOM-killed, increase Docker's available memory or stop unused workloads; changing a JVM setting cannot reserve memory for the whole Docker VM.

Check and package it without accessing a cluster:

```bash
make helm-lint
make helm-template
make helm-package
```

The package is written under `dist/charts/`. These commands do not install a release.

To publish the chart to an OCI registry after authenticating with `helm registry login`, run `helm push dist/charts/runloom-0.1.0.tgz oci://registry.example.com/charts`. Consumers can install it with `helm upgrade --install runloom oci://registry.example.com/charts/runloom --version 0.1.0 --namespace runloom --create-namespace -f values-hosted.yaml --wait`, using their reviewed hosted values. Publish the matching application images separately.

### Development cluster

Run the following from the repository root. Install Docker Desktop/Engine, kind, kubectl, Helm (3 or 4), Make and a supported Node.js version first; start the local Docker daemon. The commands do not install system tools or alter Docker Desktop resource allocations. Allow enough Docker memory/disk for the independent application/bootstrap image builds, identity services, Temporal and application databases.

```bash
make k8s-up
make k8s-open
```

`k8s-up` creates or reuses the Docker-backed kind cluster `runloom-local`, builds and loads the independent application and bootstrap images, and installs or upgrades the `runloom` Helm release in namespace `runloom`. A fresh local Make installation selects MongoDB; the chart itself still defaults to PostgreSQL. Existing releases retain their database choice. Each invocation uses a new image tag so Kubernetes rolls out changed code rather than reusing a cached `:local` image. It waits for Helm readiness, runs the chart connectivity test and ensures the local accounts on helper-managed installations while preserving existing passwords. It does not configure optional model-provider secrets, start the separate marketing website, or affect the Compose stack.

Local development installs/upgrades also create a persistent workflow-vault encryption key when no `workflowSecrets.existingSecret` is configured. The key is reused on upgrades, retained on Helm uninstall, and mounted read-only only into Workflows API and worker. Back it up separately from the database; namespace/cluster deletion removes it. This enables saving named values in **Management → Workflow secrets** without an extra operator step. Hosted releases still require an explicitly provisioned key; see [workflow secret setup](api-nodes-and-secrets.md#operator-setup).

`k8s-run` forwards the UI service on loopback and prints the browser URL, normally <http://localhost:4200>. Keep that terminal open. Ctrl+C stops forwarding only; the cluster, workloads and data remain. `/api`, `/auth` and the three remote Angular apps use the same host proxy. If an existing release uses another local login origin, forwarding follows that origin. An occupied port fails visibly rather than silently changing the login origin. Use `make k8s-docs` in another terminal to forward the separate documentation service, normally <http://localhost:4400>.

`k8s-open` is the website-to-Studio alternative to `k8s-run`. First run `make setup-website` if the independent website dependencies are not installed. It forwards the installed Studio and enabled documentation service, waits for readiness, ensures the local accounts using a one-shot bootstrap Job, builds/previews the marketing website at <http://127.0.0.1:4300>, prints connection/account information, then opens the default browser. The website's **Log in** and **Open workflow studio** links use the installed Studio origin; the Compose default in source is unchanged. Keep this terminal open. Ctrl+C stops only processes started by this command. Stop existing `k8s-run`/`k8s-docs` forwards before using it; occupied ports are reported without killing another process. To change the website port, use `K8S_WEBSITE_PORT=4500 make k8s-open`; it must differ from Studio and docs ports. `K8S_OPEN_BROWSER=0 make k8s-open` keeps the services running without opening a browser.

The explicit local bootstrap creates `admin@runloom.test` with public local-only password `RunloomLocal!123` when missing, ensures owner membership in the Runloom organization and admin membership in its General team, and also ensures `member@runloom.test` / `RunloomMember!123` in the same organization/team with initial member/editor roles. Existing passwords, the member's existing roles, and other members/data are preserved. The terminal distinguishes newly created credentials from existing accounts whose passwords may differ from the defaults. The Kubernetes helper never resets or retrieves existing passwords. The bootstrap Job uses the dedicated `images.localAdmin` image and invokes `/runloom-local-admin`; the Auth runtime no longer contains this executable. Normal API/worker startup and hosted Helm installs do not create these accounts. The Job references the installed database ConfigMap/Secret, has no Kubernetes API token, runs without root, has a bounded deadline and expires after completion. If interrupted after creation, the Job may finish its idempotent account setup; stopping the preview does not delete the accounts. Never use these public credentials outside local development.

For separate lifecycle steps:

```bash
make k8s-cluster          # Create/reuse the local cluster; no application deployment
make k8s-down             # DELETE the cluster and ALL cluster-local data
make k8s-install          # Build/load images and install; refuses an existing release
make k8s-upgrade          # Rebuild/load images and upgrade; requires an existing release
make k8s-status           # Show releases and namespace pods, services and volumes
make k8s-logs             # Follow all namespace pods, including databases and sidecars
make k8s-test             # Re-run the installed chart's connectivity test
make k8s-consumer-setup   # Optional verification/repair; already included in up/install/upgrade
make k8s-open             # Ensure local admin, print connection info and open website
make test-local-k8s       # Orchestration/safety tests; does not access a cluster
```

All Kubernetes/Helm calls explicitly target `kind-<K8S_CLUSTER>`, regardless of the selected context. The script rejects remote Docker daemons, non-loopback Kubernetes API endpoints, and hosted/ingress-enabled releases. kind may select its new context when creating a cluster. Existing release values, including optional Secret references, are preserved on upgrade; captured configuration is not printed. Setup, status and forwarding commands never delete clusters, PVCs, images or releases. They are for local development only, not staging deployment.

`make k8s-logs` follows the current pods in `K8S_NAMESPACE` (default `runloom`) using native `kubectl`, including application services, databases, infrastructure, jobs and logging sidecars. It prints the last 100 lines per container, then streams new output with timestamps and pod/container prefixes. Ctrl+C stops only the log followers. It reads every pod in the selected namespace, even if multiple releases share it. Rerun after a rollout or container restart to attach to replacement pods/containers; this command does not rediscover them automatically. For Compose, use `make docker-logs`.

`make k8s-down` permanently deletes the named Kind cluster (default `runloom-local`), including every namespace, workload, Kubernetes Secret, and cluster-local persistent volume. This removes application databases, identity accounts, Temporal history, metrics and the workflow-vault key stored in that cluster; Helm retention annotations do not protect data from cluster deletion. It works without a healthy Kubernetes API and succeeds when the cluster is already absent. Docker images, host-side credential backups, externally hosted databases and other clusters remain untouched. For another local cluster, use `make k8s-down K8S_CLUSTER=runloom-sandbox`. Back up anything needed before running it. Recreate the installation with `make k8s-up`.

Supported overrides:

| Variable | Default and behavior |
| --- | --- |
| `K8S_CLUSTER` | `runloom-local`; explicitly targets context `kind-runloom-local` |
| `K8S_NAMESPACE` | `runloom` |
| `K8S_RELEASE` | `runloom`; use another release for a separate dataset |
| `K8S_DATABASE` | MongoDB on a fresh install; preserves the installed driver on upgrades. Supports `mongodb` or `postgres`; refuses in-place driver changes. |
| `K8S_PORT` | `4200` on a fresh install; otherwise preserves the installed login origin. Choose a different port when creating a new release; changes to an existing origin are refused because Keycloak persists its client redirect URLs. |
| `K8S_TIMEOUT` | `10m`; increase for slow initial image pulls/startup |
| `K8S_WEBSITE_PORT` | `4300`; website preview for `k8s-open`, separate from the Studio port |
| `K8S_OPEN_BROWSER` | `1`; set `0` to print information and run the preview without launching a browser |
| `NODE`, `DOCKER`, `KIND`, `KUBECTL`, `HELM` | Tool executable names/paths; Node uses the same supported version range as Studio |

For a separate PostgreSQL installation on another browser port:

```bash
make k8s-up K8S_RELEASE=runloom-postgres K8S_DATABASE=postgres K8S_PORT=4305
make k8s-run K8S_RELEASE=runloom-postgres
```

Repeat non-default cluster, namespace and release settings on subsequent commands. The database and browser origin are read back from the installed release. Switching a database driver is not a migration; create a separate release instead. Authentication and application data in Kubernetes are separate from Compose. The guarded local helpers ensure development accounts; ordinary hosted Helm install/upgrade does not seed default accounts. Local setup also ensures separate platform administrator and restricted staff accounts; see [platform administration](platform-administration.md).

`k8s-test` checks UI-to-API connectivity and Temporal/database health, not a full authenticated workflow. For the broader `make smoke` test, supply `RUNLOOM_ACCESS_TOKEN_FILE` and `RUNLOOM_TEAM_ID` as described in [authentication testing](authentication.md#validation), and point its base URL to the forwarded Studio. That test creates synthetic records and may start/cancel a run.

On a failed install/upgrade, the command exits without reporting success or deleting diagnostic state. Inspect `make k8s-status`, then the pod logs/events using the explicit local context. Initial API/worker restarts while dependencies start are possible; increasing the Helm wait budget does not change that application behavior. If an upgrade needs reverting, inspect `helm history runloom --kube-context kind-runloom-local --namespace runloom` and explicitly select the prior revision for `helm rollback`. A rollback does not reverse database changes. Do not delete/recreate a cluster to resolve a probe warning: cluster deletion also destroys kind's local volume data.

Temporal's development UI can be forwarded separately:

```bash
kubectl --context kind-runloom-local -n runloom port-forward --address 127.0.0.1 service/runloom-temporal 8233:8233
```

For a remote cluster, publish every enabled component's independent image and required bootstrap images to a registry reachable by its nodes. Use the hosted Helm instructions below. Local Docker images are not automatically available to Kubernetes; the local Make commands deliberately do not deploy to remote contexts.

### Hosted deployment with external services

Use [values-production.yaml](../charts/runloom/values-production.yaml) as a template. Set real image repositories and immutable release tags, the reachable Temporal frontend, namespace, database Secret name, and your Ingress host/TLS Secret. This is a configuration example, not production infrastructure provisioned by the chart.

Build and publish the independent images described in [service image configuration](service-split.md#4-helm-and-externally-supplied-databases). Configure each matching `images` entry, all four Studio images, and `docs.image` when documentation is enabled. There is no shared `runloom-backend` image. The marketing website remains an independent static build without a Docker image.

Provision separate database URL Secrets for Auth, Workflows, Management and enabled Assistant, plus workload-token Secrets and the trust ConfigMap. The worker shares only the Workflows database Secret. Follow the [service database and credential prerequisites](service-split.md#4-helm-and-externally-supplied-databases); a single shared `runloom-database` Secret is not sufficient. Use your normal secret-management process; do not store credentials in committed Helm values.

Temporal connections support TLS and optional client certificates through `temporal.tls`. Enable it for hosted connections and supply the mounted CA/client-certificate Secret as appropriate. API-key authentication is not implemented. The namespace must already exist. Configure the same task queue, payload keyring and TLS settings for Workflows API and worker. Local development keeps plaintext transport on its private network; this is not a hosted-security configuration.

## Execution security configuration

Fresh Compose service installations and Helm installations reuse their persistent workflow-vault key to derive a separate execution-encryption key. Key bytes are never printed or stored in images/ConfigMaps. Workflows and worker must receive the same keys. Explicit PostgreSQL/MongoDB runtime configuration without a payload keyring or vault key fails startup.

An optional JSON keyring has the shape `{"active":"key-2026-10","keys":{"key-2026-10":"<base64-encoded 32-byte key>","previous":"<retained old key>"}}`. Supply it as a private mounted file using `RUNLOOM_PAYLOAD_KEYRING_FILE`, or Helm `executionSecurity.existingKeyringSecret` and `keyringSecretKey`. Compose supports the additional [security overlay](../compose/compose.security.yaml), with `RUNLOOM_PAYLOAD_KEYRING_PATH` pointing to that private file. The overlay mounts a YAML configuration fragment, not secret-valued environment variables. Default local Make startup uses the derived key and needs no extra file.

Rotation is a coordinated operation:

1. Back up current key material separately from application databases. When first switching from the vault-derived key, retain its derived bytes under `vault-derived-v1`; derivation is HMAC-SHA256 with the raw vault key and message `runloom/execution-encryption/v1`. Never change the vault key to rotate execution encryption: the vault still needs its original key.
2. Distribute a keyring containing both old and new keys to every API and worker, with the old key still active. Restart all readers; keyrings are startup snapshots, not hot-reloaded.
3. After every reader knows the new key, change `active` and restart writers. New payloads use the new key; old stored data and replay histories still require their previous keys.
4. Do not remove old keys until all dependent execution histories, records and backups have expired or been explicitly migrated. This release does not implement automatic re-encryption or KMS envelope wrapping.

Upgrading an existing plaintext installation requires a planned maintenance window and `executionSecurity.allowLegacyPayloads: true` / `RUNLOOM_ALLOW_LEGACY_PAYLOADS: "true"` on API and worker. This is an explicit compatibility exception: it permits old plaintext reads, but every new write remains encrypted. It does not rewrite historical data. Stop old writers before cutover; old binaries cannot read new ciphertext. Do not disable compatibility until old records/histories have been migrated or expired. Missing keys and corrupted encrypted envelopes never fall back to plaintext. Fresh installations reject plaintext payloads by default.

Set `auth.requiredACR` / `RUNLOOM_OIDC_REQUIRED_ACR` and `auth.maxAuthAge` / `RUNLOOM_OIDC_MAX_AUTH_AGE` only after configuring Keycloak's real authentication flow and signed access-token `acr`/`auth_time` claims. The services verify those claims after signature/issuer/audience checks; a configured string alone is not MFA. Missing, wrong, stale or future authentication claims are rejected. This release enforces freshness globally when enabled, rather than adding a per-action Studio step-up flow. Local accounts do not have MFA enabled automatically.

Temporal SDK logs suppress free-text messages and arbitrary error attributes; warnings/errors emit categorical records. Workflow-service audit failures include a server-generated request identifier. HTTP 5xx responses omit underlying error details. Authorized users may still see execution errors in protected run data. Do not enable proxy/APM request-body, authorization-header or response-body capture; those are outside the application logger.

The three application services still rely on private networking plus platform-provided TLS/mTLS for hosted internal transport; this release does not install a mesh or add inbound application TLS. Database URL TLS options, encrypted disks/backups, ingress TLS and provider configuration remain deployment requirements. Do not label the local plaintext Compose network a compliant production environment.

Set `development.enabled=false` to exclude all bundled infrastructure and the development database Secret. Supply `auth.url`, `auth.issuer`, and `auth.jwksURL` for an externally managed Keycloak deployment, configure the public client with PKCE/audience/origins, and configure production SMTP as described in [authentication](authentication.md). Set `database.driver=mongodb` and a MongoDB URL Secret if needed. Filesystem storage is intentionally rejected by the chart.

Optionally set `llm.existingSecret`, `llm.secretKey`, and `llm.defaultModel`. The key is projected to a read-only file; it is not in the workflow definition or Temporal input. Restart application Deployments after rotating database credentials; ConfigMap changes trigger a rollout automatically.

Deploy the reviewed values:

```bash
helm upgrade --install runloom charts/runloom --namespace runloom --create-namespace -f charts/runloom/values-production.yaml --wait --timeout 5m
helm test runloom --namespace runloom
```

Ingress requires an installed controller and a provisioned TLS Secret. It sends browser and API traffic to the UI proxy, keeping the browser on one origin. All workflow, callback, signal, and webhook endpoints require verified user tokens and team roles. Keep Temporal, databases, identity administration, and Mailpit private. Rate limits, integration replay protection, backups, audit retention, activity idempotency, and Temporal worker compatibility remain operator responsibilities.

### Upgrades and persistence

Use a distinct immutable image tag for each release. `helm upgrade` performs rolling application updates. API shutdown allows up to 20 seconds for active HTTP requests; worker shutdown allows up to 30 seconds for in-flight activities. Pods have a 45-second termination grace period.

Do not assume an arbitrary worker image rollback is replay compatible. Changes to workflow command sequences require patching or worker versioning and history replay tests. This chart does not automatically enable Temporal Worker Deployment Versioning.

The development StatefulSet PVCs remain after `helm uninstall`; manage their backup and eventual deletion separately. External databases and external Temporal are never owned by this chart. Changing from bundled to external services or switching database drivers is not a data migration.

Staging deployment is a separate process from building, packaging, or reviewing this chart. Follow the repository's staging procedure when a staging deployment is explicitly requested.

## Health and troubleshooting

| Component | Liveness | Readiness |
| --- | --- | --- |
| API | `GET /healthz` on 8080 | `GET /api/v1/health`; HTTP 503 if Temporal or the database is unavailable |
| Worker | `GET /healthz` on 8081 after worker startup | `GET /readyz`; checks Temporal and database connectivity |
| UI | `GET /healthz` on 8080 | Same local Nginx check |

UI readiness does not imply API readiness. Check `/api/v1/health` through the UI and run the smoke test for the complete path. Worker readiness verifies startup and connectivity, not ongoing task-queue throughput. Startup dependency failures cause API/worker exits; Compose and Kubernetes restart them.

Use `docker compose --project-directory . -f compose/compose.yaml logs api worker ui` locally. On Kubernetes use `kubectl -n runloom get pods,pvc`, `kubectl -n runloom describe pod <pod>`, and `kubectl -n runloom logs deployment/runloom-api`. A Pending PVC usually needs an available StorageClass; ImagePullBackOff needs a reachable image/tag and any required `imagePullSecrets`. Readiness failures commonly indicate missing database credentials, an unreachable Temporal endpoint, or a namespace mismatch.

For probe behavior and Compose file reuse, see the [Kubernetes probe documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/) and [Docker Compose include documentation](https://docs.docker.com/reference/compose-file/include/).
