# Runloom

Platform administrators and restricted staff use the independent Angular [Platform Admin app](studio/projects/platform-admin/README.md) at `/platform`. Platform access is separate from organization/team roles. See [platform administration and setup](docs/platform-administration.md).

For a fresh independent-service installation with Auth, Workflows, Management and Assistant databases, use `make local-services-up SERVICES_DATABASE=mongodb` or `SERVICES_DATABASE=postgres`. See [service ownership, local URLs and Helm configuration](docs/service-split.md). The standalone Go token-verification SDK lives in `sdk/go-auth/`.

Runs include [LLM cost and token accounting](docs/llm-costs.md), with versioned pricing, retry-aware records, child-run totals and a Studio Cost & Usage view. The **Cost analytics** dashboard adds role-scoped spend trends, KPIs and sortable run/team/model breakdowns for daily through yearly reporting windows. [Email nodes](docs/email-node.md) send through configured SMTP; local Compose routes messages to Mailpit.

Custom Code nodes support isolated, JavaScript-only JSON transformations with an inline Studio editor and server-side tests. See [Custom Code](docs/custom-code.md) and the [order example](examples/javascript-transform.json).

Backend integrations have a separate [consumer execution API](docs/consumer-api.md): Keycloak service accounts, one-time secret display, explicit workflow-version grants, idempotent starts, owned-run listing/status/cancellation, redaction and quotas. OpenAPI is served at `/api/v1/openapi.json`; optional identity provisioning setup is documented in that guide.

API documentation is separated into the [Studio UI and management API](docs/studio-api.md) and [consumer execution API](docs/consumer-api.md), each with its own authentication, permissions, examples and errors. The [API index](docs/api.md) compares supported operations. Consumers can list, start, inspect and cancel granted/owned runs; consumer pause/resume is not implemented. Human-authorized Studio resume is documented separately.

Interactive Swagger pages are served at `/api/docs/studio/` and `/api/docs/consumer/` on your Studio/API origin. Download OpenAPI from `/api/v1/studio/openapi.json` and `/api/v1/openapi.json`. They ship inside the Go API, including Docker/Helm builds, without an external documentation service. Existing installations need an API rebuild/upgrade. Maintenance commands: `make setup-api-docs build-api-docs test-api-docs`.

Runloom lets non-technical users design, publish, and run durable workflows from validated JSON definitions. The repository contains a standalone Angular studio and a Go backend:

1. `studio/` groups four independent Angular applications: the authentication host, workflows builder/run console, management app and Platform Admin app. Each has its own install, build, tests, Docker image and output directory. The separate `studio/libraries/platform` project provides the packaged auth/UI platform library. See [project boundaries and commands](studio/README.md).
2. `server/auth`, `server/workflows`, `server/management` and `server/assistant` are independent Go services with separate modules, builds, tests, Dockerfiles and owned databases. The Temporal worker belongs to Workflows and shares its database. The stateless gateway and token-verification SDK are separate projects.
3. `Website/` is an independent Angular product website. It explains the product and runs a browser-only demo without the studio or backend.
4. `docs-site/` is an independent Angular documentation website with its own build and image. It serves guides and API references at a separate address, not a Studio subpath.

The public JSON contract remains separate from runtime code so definitions stay validated, portable, and suitable for external tooling.

The project, Helm chart, and default Compose project are named `runloom`; the builder is **Runloom Studio**. See [the rename and migration guide](docs/rebranding.md) before reusing an older installation's data or infrastructure.

## License and contributions

Original project code, documentation, schemas, and examples are available under the [MIT License](LICENSE). Third-party dependencies and services retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md), including the distinction between the MongoDB driver and Community Server.

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, checks, workflow design rules, and the pull request process.

For a ready-to-use local account, run `make local-up` and open http://localhost:34200 (MongoDB) or http://localhost:35200 (PostgreSQL). See [local startup and administrator commands](docs/local-development.md) for the public development credentials and password-reset command. Guarded local Compose/Kind setup ensures these accounts; normal server startup and hosted Helm deployments do not create default accounts.

## What works in version 1

1. n8n-style visual canvas, node library, parameter inspector, and advanced JSON editor.
2. Draft validation and immutable published versions.
3. Activity, AI/LLM, AI Decision (one action, several actions, or bounded repeated decisions), durable wait, external callback, named signal, human approval, operator pause, condition, and parallel nodes. See the [AI Decision contract](docs/json-contract.md#ai-decision) and [three-mode example](examples/ai-decision-modes.json).
4. Input and prior-step value interpolation.
5. Configurable activity timeouts and retries.
6. Manual and webhook triggers plus live run status, approval decisions, callback and signal simulation, pause/resume, and permanent cancellation.
7. Temporal-backed crash recovery and replay-safe orchestration.
8. PostgreSQL, MongoDB, or filesystem persistence for definitions, run inputs, status/output snapshots, and audit events.
9. Draft commit history, immutable published releases, actor-attributed commit messages, and restore-as-new-draft controls.
10. Email/password sign-in, email verification, password recovery, and configurable Google login through Keycloak.
11. Organizations, teams, server-enforced roles, team-isolated workflow data, and membership audit history.
12. Recurring and one-time schedules with time zones, saved input, pause/resume and overlap protection.
13. Reusable published subworkflows with pinned versions, mapped input/output, parent/child run links, cancellation and audit history.
14. Attempt-level LLM usage and estimated costs with versioned pricing, MongoDB/PostgreSQL persistence and per-run Studio breakdowns.
15. SMTP Email nodes with interpolation, plain text/HTML alternatives and local Mailpit delivery.

See [schedules and subworkflows](docs/schedules-and-subworkflows.md) for the builder walkthrough, API contract, examples, execution rules and live integration tests.

Try the [runnable sample pack](examples/README.md): reusable children, sequential and parallel parent calls, hourly/weekday schedules, and a fresh one-time schedule generator with Studio/API instructions. In **Workflows**, select **Example → Parallel child workflows** and **Try a guided example** to prepare a parent draft with two parallel child runs and prefilled test input.

The example activity catalog includes `runloom.echo`, `runloom.compose`, and `runloom.sum`. Definitions can also configure [External API nodes](docs/api-nodes-and-secrets.md), [Database nodes](docs/database-connections.md) and bounded, sandboxed [JavaScript Custom Code nodes](docs/custom-code.md). These capabilities execute through worker activities, never directly in deterministic workflow code. Definitions reference stored credentials by name or connection ID; secret values must remain outside workflow JSON. Additional integrations belong in registered activities with validation and execution limits.

The `AI / LLM` node calls an OpenAI-compatible chat-completions endpoint as a retryable activity. Every new LLM response must include a validated `hitl_required` boolean and `hitl_reason`; use a strict JSON Schema-compatible model. Ordinary answer text remains in `output.content`. See the [LLM output contract](docs/json-contract.md#ai--llm) for routing human review and compatibility. Configure the worker with `RUNLOOM_LLM_BASE_URL`, `RUNLOOM_LLM_DEFAULT_MODEL`, and either `RUNLOOM_LLM_API_KEY_FILE` or `RUNLOOM_LLM_API_KEY`. Prefer the file option in deployed environments. The credential is never part of workflow JSON or Temporal input.

## Quick start

For the independent documentation portal, run `make setup-docs` then `make run-docs` and open http://127.0.0.1:4400. It includes getting-started tutorials, searchable guides, light/dark themes and separate OpenAPI references. `make build-docs` produces static pages without a runtime server or containers. See [documentation website development](docs-site/README.md).

Studio's **Documentation** menu opens the separate docs address in a new tab. Compose includes docs on port 34400 (MongoDB) or 35400 (PostgreSQL); `make docker-up-docs` runs docs alone on 4400. Helm includes an independent docs service with configurable `docs.publicURL` and optional `docs.ingress`; `make k8s-docs` forwards it locally. The docs app retains its own build and image, with no Studio build dependency.

For the standalone product website, run `make setup-website` then `make run-website` and open http://127.0.0.1:4300. Build static files with `make build-website`. See [website setup and hosting](Website/README.md); no website container configuration is provided.

Start the complete independent-service stack with either database:

```sh
make local-services-up SERVICES_DATABASE=mongodb
# Or a separate PostgreSQL installation:
make local-services-up SERVICES_DATABASE=postgres
```

Open the Studio URL printed by setup. Keycloak and the local Studio administrator are provisioned during setup. See [service setup](docs/service-split.md) for local credentials, ownership, ports and configuration. Each service can also be built independently with `make -C server/auth build`, `make -C server/workflows build`, or `make -C server/management build`.

## Make commands

Run `make` or `make help` from the repository root to see available commands. Existing `docker-*-go` aliases remain supported.

Run `make zip` to create `dist/runloom-source-001.zip`, then `002`, and so on using Git and `zip`. Each run uses one more than the highest existing numbered archive and refuses to overwrite an existing file. It includes current tracked files and new non-ignored files and respects nested ignore rules for untracked files. As with `git ls-files`, already-tracked files remain included even if subsequently ignored. Filenames containing newlines are not supported.

Source code lives in the four independent owner services and stateless gateway under `server/`, plus `sdk/go-auth/`, and `studio/`. `make build-server` and `make test-server` orchestrate backend builds and tests without creating build dependencies between services. Binaries are written to each project's own `bin/`. Image names and Helm image keys changed with this service split; see the deployment guide.

```sh
make setup                 # Install backend and Studio dependencies
make build                 # Build independent service binaries and Studio
make test                  # Go tests and Studio production build
make verify                # Static checks, race tests, Studio build, Compose and Helm checks
make test-consumer         # Consumer authentication and API package tests
```

`make verify` requires Go with race-detector support, Node/npm, installed Studio dependencies, Docker Compose, Helm and `workflowcheck` installed in `$(go env GOPATH)/bin`. It validates deployment configuration without starting or deploying the stack. Ordinary tests skip live integrations unless their `RUNLOOM_TEST_*` opt-in variables are set. The independent website remains separate: use `make setup-website test-website` to install, test and build it.

Browser checks (`make test-ui-browser`) require the running Angular development server and Playwright; see [UI testing prerequisites](studio/README.md). Explicit live checks are `make test-keycloak-live` (creates/removes a disposable Docker container) and `RUNLOOM_TEST_TEMPORAL_ADDRESS=127.0.0.1:7233 make test-temporal-live` (creates test executions and schedules on that server). Use disposable infrastructure, never production.

Service-account setup and consumer-enabled Compose targets are documented in [consumer operator setup](docs/consumer-api.md#make-shortcuts). They accept private credential file paths, not secret values. Deployment and credential provisioning are always explicit commands, not prerequisites of `make build`, `make test`, or `make verify`.

## Docker

The Docker Compose stack includes the independent backend services and gateway, Workflows worker, four Studio apps, documentation, a Temporal development server, the selected application database, Keycloak with its own PostgreSQL database, Mailpit for development email, and persistent volumes. MongoDB is the default application database. Use `SERVICES_DATABASE=postgres make docker-up` to select the separate PostgreSQL installation.

```bash
make docker-up
```

Open the workflow studio at [http://localhost:4200](http://localhost:4200), the backend API at [http://localhost:8080](http://localhost:8080), and the Temporal UI at [http://localhost:8233](http://localhost:8233). Stop the stack without deleting its data:

```bash
make docker-down
```

Build the Go backend and Angular images without starting containers:

```bash
make docker-build
```

The default image names are `runloom-backend:local` and `runloom-ui:local`. Override them with `BACKEND_IMAGE` or `UI_IMAGE` when needed.

The `compose/compose.yaml` file starts the complete stack and waits for health checks. It binds host ports to localhost. Register in the studio and open verification/reset emails at http://localhost:8025. Use `make docker-logs` to follow logs; `make smoke` now requires a signed-in token and team ID as described in [authentication testing](docs/authentication.md#validation). The exposed ports can be changed when another service is already using them:

```bash
RUNLOOM_API_CONFIG_PATH=./compose/config/api.local.yaml UI_PORT=14200 API_PORT=18080 TEMPORAL_PORT=17233 TEMPORAL_UI_PORT=18233 make docker-up-go
```

The existing `docker-*-go` Make targets remain aliases. See [the deployment guide](docs/deployment.md) for MongoDB selection, secrets, port overrides, and persistence.

## Kubernetes and Helm

The [Runloom chart](charts/runloom) deploys the UI, API, and worker independently. Default values include development Temporal and database StatefulSets with persistent volumes; the hosted example uses external services and an existing database Secret.

```bash
make helm-lint
make helm-package
```

For a local kind cluster, with Docker running and kind, kubectl, Helm and Node.js installed:

```bash
make k8s-up       # Create/reuse cluster, build/load images, install/upgrade and health-test
make k8s-open     # Print local admin info; open website linked to Studio
```

Use `make k8s-upgrade` after code/chart changes, `make k8s-status` to inspect the local stack, and `make k8s-test` for its connectivity check. Separate `k8s-cluster` and `k8s-install` commands are also available. Fresh local Make installations use MongoDB; upgrades preserve the existing database and settings. All commands explicitly target the named local kind cluster, not the currently selected remote context. `k8s-open` needs website dependencies (`make setup-website`) and explicitly creates the local admin if missing, preserving existing passwords. Keep its terminal open; use `make k8s-run` instead when you only need Studio forwarding.

To completely remove the local Kind cluster, run `make k8s-down`. This permanently deletes all workloads, cluster-local databases, accounts and Kubernetes Secrets in that cluster. Images, host credential backups and other clusters are kept. Override the target with `K8S_CLUSTER=your-cluster`; see [local cluster lifecycle](docs/deployment.md#development-cluster).

Use [the deployment guide](docs/deployment.md) for registry publishing, external-service configuration, Ingress/TLS, health checks, Helm tests, and upgrade rules. Follow the [self-hosted version upgrade guide](docs/upgrading.md) when upgrading an existing installation. Bundled Temporal is development-only; the hosted configuration currently supports a private self-hosted Temporal frontend, not Temporal Cloud.

## Builder execution rules

The builder includes **External API** nodes, encrypted **Workflow secrets** in the sidebar under Workspace settings, and explicit **End workflow** nodes. Workflows also finish automatically after the final node. See [API calls, secrets and completion](docs/api-nodes-and-secrets.md) for setup, JSON examples, retry behavior and security boundaries.

**Management → Connections** provides encrypted team database connections for MongoDB, PostgreSQL, MySQL and Supabase. **Database** nodes support schema discovery, field mapping, bound SQL and MongoDB JSON operations. See [database connections](docs/database-connections.md) for setup, permissions, limits and live connector verification.

1. A wait is a durable timer. It continues automatically when its duration ends and needs no response.
2. A callback waits for an external system to send JSON for the exact workflow run and step. The JSON becomes that step's output.
3. A named signal waits for a business event name such as `documents.ready`. JSON sent to that name becomes the signal node's output.
4. An approval waits for a person to approve or reject. It records the decision, comment, and actor.
5. A pause creates an operator checkpoint. Resume advances only that checkpoint and may carry optional JSON data.
6. A cancellation closes the run permanently. It cannot be resumed.
7. Callback, named signal, approval, and resume messages are durable, so they survive worker restarts and can arrive before the workflow reaches their node.
8. A webhook trigger starts the latest published version at `POST /api/v1/studio/webhooks/{definition-id}/{path}`. Its JSON body becomes workflow input.

## Integration endpoints

See the [Studio workflow execution guide](docs/workflow-api.md) for human-authorized start, status, resume and cancellation requests. External applications use the separate [consumer API reference](docs/consumer-api.md).

1. Send a callback with `POST /api/v1/studio/callbacks/{workflow-id}/{step-id}` and JSON shaped as `{"payload": {...}, "actor": "system-name"}`.
2. Send a named signal with `POST /api/v1/studio/runs/{workflow-id}/signals/{signal-name}` and the same payload shape.
3. All workflow endpoints require a verified user access token and `X-Runloom-Team`. Mutations require team editor/admin access. Supplied actor values are overwritten with the verified user identity. Machine credentials, public webhook signatures, rate limits, and replay protection remain future integration work; see [authentication](docs/authentication.md).

## Database and run history

1. `RUNLOOM_DATABASE_DRIVER` accepts `postgres`, `mongodb`, or `file`.
2. Mounted application YAML uses `RUNLOOM_DATABASE_URL_FILE` to read a separately mounted database credential. Compose selects its host file with `RUNLOOM_DATABASE_SECRET_PATH`; Helm uses `database.existingSecret`. Native execution without `RUNLOOM_CONFIG_FILE` still accepts `RUNLOOM_DATABASE_URL`. `RUNLOOM_DATABASE_NAME` selects the MongoDB database and defaults to `runloom`. See [mounted configuration](docs/deployment.md#mounted-application-configuration).
3. `GET /api/v1/studio/runs?limit=50` returns stored workflow executions including their original inputs and latest status/output snapshot.
4. `GET /api/v1/studio/runs/{workflow-id}/history` returns one stored execution and its audit timeline.
5. `GET /api/v1/studio/audit` supports `workflow_id`, `definition_id`, and `limit` filters.
6. The API creates the required PostgreSQL tables or MongoDB collections and indexes at startup. Published definition versions are immutable in every backend.
7. A background reconciler refreshes stored run status and output, so history completes even when no UI is polling.
8. Temporal Event History remains the replay authority. Database audit writes are product-facing records and are not transactionally atomic with Temporal messages; use idempotency and an outbox-style integration for strict production guarantees.

See `docs/database.md` for the data model, retention boundary, and production guidance.

## Workflow versions and commits

Open **Management → Prompt library** to search, create, edit and publish reusable team prompts and inspect revision history. LLM and AI Decision nodes retain a quick **Prompt library** picker to pin published versions, typed input mappings, variable autocomplete, previews and paid test runs. **Management → Run history** is the separate dashboard for recent workflow executions, filters, inputs, outputs and audits; the workflow builder retains its current-test panel. See [typed prompts and prompt library](docs/json-contract.md#typed-prompts-and-prompt-library) for usage, permissions and limits.

1. Every save creates an immutable draft commit with the full JSON snapshot, actor, message, sequence, and timestamp.
2. Publish creates the next immutable numbered version and a linked publish commit.
3. Restore copies a historical snapshot into a new draft commit; it never rewrites a published version.
4. `GET /api/v1/studio/definitions/{definition-id}/history` returns the combined save, publish, and restore timeline.
5. The studio opens this history from the version badge and can view published releases or restore any commit.

See `docs/versioning.md` for lifecycle rules, endpoints, storage semantics, and the boundary from Temporal worker-code versioning.

## Contract and architecture

Read `docs/product.md`, `docs/architecture.md`, `docs/database.md`, `docs/versioning.md`, and `docs/json-contract.md`. See [projects and workflow folders](docs/projects.md) for team → project → workflow organization, project APIs, permissions, and storage behavior. The machine-readable contracts are `spec/workflow.schema.json` and `spec/schedule.schema.json`. `examples/employee-onboarding.json` covers the original nodes; `examples/parent-with-subworkflow.json` calls the separately published reusable greeting.

## Production boundary

The local Temporal server, bundled databases, Keycloak `start-dev`, and Mailpit are development defaults. Hosted deployments require a production-grade self-hosted Temporal cluster (Cloud connectivity is not yet implemented), backed-up databases, production Keycloak with HTTPS/SMTP, audit retention, secrets management, observability, activity idempotency, and worker versioning with replay tests. Team-level authorization is implemented; per-approval policies and custom roles are not.
