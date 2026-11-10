# Architecture

The independent Auth, Workflows, Management and Assistant processes and fresh MongoDB/PostgreSQL setup are described in [Backend service split](service-split.md). Each owner has a separate database; the Workflows API and worker share the Workflows database. The stateless gateway owns no database. The fresh service installation does not import existing monolith data.

The account boundary uses Keycloak OIDC with browser PKCE. The stateless gateway routes requests to their owning services; it does not verify tokens or open stores. Destination services authenticate and authorize public requests, using the independent token-verification SDK and Auth's permission contracts as appropriate. Keycloak owns login credentials; Runloom Auth owns organization/team memberships and access records. See [service authentication and discovery](service-split.md#2-authentication-and-discovery).

Backend consumers use a separate execution API under `/api/v1`, routed through the gateway to Workflows; Studio and management routes use distinct authorization rules. Keycloak issues client-credentials tokens for the execution audience. Auth owns service-account access records, while Workflows enforces execution permissions and owns consumer admission/idempotency records. Runloom checks the registered service-account subject, credential generation, scopes and workflow-version grants. A durable compare-and-swap ledger reserves idempotency keys and root-run quotas before dispatching the existing workflow type. Immutable payloads are stored separately from the bounded admission index. Public responses expose only permitted fields and consumer-owned roots, not interpreter state. See [consumer execution, identity setup and limits](consumer-api.md).

## Runtime shape

```text
Browser
  -> Angular auth/navigation host
       -> federated Angular workflows app
       -> federated Angular management app
       -> federated Angular Platform Admin app
   Studio and consumer API requests
     -> Stateless gateway
          -> Auth       -> Auth database
          -> Workflows  -> Workflows database
          -> Management -> Management database
          -> Assistant  -> Assistant database (when enabled)
   Workflows API -> Temporal service <- Workflows worker
                                       -> JSON interpreter and activities
                                       -> Workflows database
   Documentation -> independent Angular docs site on a separate origin
```

Studio contains four independent Angular projects under `studio/projects/`: `host`, `workflows`, `management` and `platform-admin`. Each has its own package/lockfile, compiler configuration, tests, Dockerfile and dist, with no sibling or parent build dependencies. The independent `studio/libraries/platform` project packages `@runloom/studio-platform`; consumers install a compiled release from their own vendor directory. The host owns the Keycloak session and loads same-origin Module Federation remotes after sign-in. Runtime singletons carry auth/context, themes, dialogs and guarded navigation contracts. Each application has its own image/deployment; the host reverse-proxies remote assets. Independent builds still require compatible runtime Angular and package contracts. See [Studio build, federation and compatibility rules](../studio/README.md#production-build).

Each Go service under `server/` has its own module, dependencies, build and Docker context; services do not import sibling source or read another owner's database. Studio applications use relative `/api` routes through the included reverse proxy. Remotes are trusted application code, not separate authorization domains; server-side team, organization and platform-access checks remain mandatory.

## Deterministic execution

The API resolves the published snapshot before starting a run and sends that complete snapshot to Temporal. Workflow code performs only deterministic traversal, value resolution, comparisons, Temporal activities, Temporal timers, and Temporal signal waits. Callback, named signal, approval, and resume messages are durable Temporal signals. Filesystem access, model calls, network access, and wall-clock access stay in activities or API adapters outside workflow code.

Parallel branches receive isolated copies of prior results. They execute concurrently, then merge results in definition order. This makes result collision behavior predictable and replay-safe. Validation rejects duplicate step identifiers across the complete definition, so branch merges cannot overwrite one another.

## Definition lifecycle

```text
editable draft -> validate -> immutable version 1 -> run
                       |-> later draft edit -> immutable version 2 -> run
```

The selected application store writes one draft and numbered published snapshots. PostgreSQL uses a row lock while assigning the next version; MongoDB and the filesystem enforce a unique definition/version key. A run embeds its snapshot; later draft or publishing changes cannot affect it.

## Two histories with different jobs

Temporal Event History is authoritative for replay and durable execution. Workflow code never reads a database directly. Activities can read application state, including secrets and budget policies, and return decisions that Temporal records in history. In particular, budget decisions can pause a workflow. The application database also provides search, reporting, original inputs, status/output snapshots, and audit screens.

The API records a run immediately after Temporal accepts it. Reading live status refreshes the application snapshot and appends a status-change audit event. Approval, callback, signal, resume, cancellation, draft, and publish actions append actor-attributed events. `GET /api/v1/studio/runs/{workflow-id}/history` combines the run record and its audit events without replacing Temporal history.

Definition commit history is also application data. Each draft save, publish, and restore stores a complete JSON snapshot. Publishing links a commit to an immutable numbered version. Restore produces a new draft and commit rather than moving or deleting existing history. Running workflows are unaffected because their exact published definition is already embedded in Temporal input.

## Extension model

Scheduled starts use native Temporal Schedules. Reusable child definitions are resolved and pinned before execution, embedded in the run input, and executed as native child workflows. API and worker share team-scoped storage; a worker activity records scheduled/child inputs and audits before business work begins. See [scheduling and child lifetime rules](schedules-and-subworkflows.md).

An integration is a named Temporal activity registered by the Go worker and the definition catalog. Activity input and output must be JSON-compatible. Side effects belong in activities, use idempotency keys derived from workflow and step identifiers, classify permanent errors as non-retryable, and heartbeat when work is long-running or cancellable.

The built-in `runloom.llm` activity is intentionally a worker-side adapter. The immutable node configuration and resolved prompt are recorded in workflow history, while the provider URL, model default, and credential come from worker configuration. This keeps provider I/O out of replay and secrets out of definitions.

## Production hardening

1. Use managed PostgreSQL or MongoDB with backups, point-in-time recovery, encryption, and tested restoration.
2. Configure and verify the existing organization/team roles, platform-access separation and consumer grants. Custom roles and per-node approval policies remain outside the current product scope.
3. Keep credentials in a secrets manager and inject them into worker activities, never definitions.
4. Add request and payload limits, audit retention and redaction policies, rate limits, and per-tenant task-queue fairness.
5. Use Temporal worker deployment versioning, saved-history replay tests, metrics, traces, and alerting.
6. Store large inputs and outputs in object storage and pass references through Temporal history.

## Independent services (2026-10-11)

Auth, Workflows and Management are now independent Go modules and Docker build contexts under `server/`. Each has its own database credentials, source, tests, dependency snapshot and build commands. The gateway and token-verification SDK are separate projects. Services use authenticated, versioned HTTP contracts; no service imports sibling code or reads another owner's database. See [the current service architecture and verification](service-split.md).

The notes below record the earlier modular-monolith design and its remaining distributed-consistency considerations. They are historical, not the current deployment topology. The service split does not add a distributed transaction or transactional outbox.

## Historical module boundaries and extraction readiness

Implemented foundation, verified on 2026-10-02: one Go deployment remains a modular monolith, not a collection of independently deployable services. [Storage ports](../server/workflows/internal/store/ports.go) separate catalog, projects, runs, audits, metering, policies, and lifecycle dependencies. API handlers select the relevant port through `Modules`; the worker's recording and metering activities receive narrower consumer-owned interfaces. No network hop or second workflow engine is introduced.

| Owner | Persistent records | Boundary |
| --- | --- | --- |
| Access | Users, organizations, teams, memberships, membership events | `access.Directory` and identity repository scope |
| Secrets | Encrypted workflow credentials and credential events | `access.SecretVault` and secrets repository scope |
| Consumer credentials | Service-account grants and lifecycle events | Consumers repository scope; Keycloak owns credential verification |
| Catalog | Drafts, published snapshots, commit history, project membership, edit leases | `Catalog` and `Projects` ports |
| Execution | Run input and status projection, consumer admission/idempotency records | `Runs` port and consumer admission repository |
| Metering | Direct provider attempts, monthly spend, budget policies and policy history | `Metering`, `BudgetReader`, and `Policies` ports |
| Audit/reporting | Product audit records and composed read views | `Audits` port; read-only API composition |

Metadata is normalized into entity records, with separate revision scopes for identity, secrets, consumer credentials, projects, edit leases, policies, and each consumer admission ledger. A change only writes changed entities. Membership and policy audit events commit with their owning module's update. Composed account/project views are not cross-module transactional snapshots. A future service must preserve authorization checks and implement its own owner-local transactions; do not reach into another owner's SQL tables or MongoDB collections.

MongoDB definition saves, publishes, and restores commit their snapshot, sequence, and commit history in one transaction. Both database adapters aggregate monthly budget spend server-side. Consumer rate counters have their own revision scope. Run reconciliation pages all unfinished records and rejects stale observations or terminal-state regressions. Worker store leases reuse at most 16 team pools and evict only idle handles; busy capacity returns a retryable activity error.

This is not yet full service extraction. Concrete adapters and contracts still share the `store` package; API team pools are still cached per team; metadata updates and admission checks still read their owner aggregate. Startup still provisions database structures. Studio start and Temporal message/audit writes do not yet use a transactional outbox, and each API replica still runs reconciliation. Extracting execution next requires durable Studio admission/dispatch, an outbox and idempotent consumers, explicit schema provisioning, coordinated reconciliation, and service-level authorization contracts. These are remaining implementation work, not guarantees of this foundation.

## Refactor verification

On 2026-10-02 the working tree passed the Go suite and race detector with live PostgreSQL/MongoDB test URLs, `go vet`, and the Temporal workflow determinism checker. Regression coverage includes module ownership, compare-and-swap concurrency, MongoDB publish rollback, budget aggregation, bounded worker leases, stale observation rejection, and pagination beyond 200 unfinished runs. Live Temporal tests passed with each database for consumer execution, schedules/children, LLM usage and budget pauses using a loopback fake provider.

The isolated `runloom-fresh` Compose installation built API/worker, Studio and bootstrap images, started successfully with a MongoDB replica set, and passed browser login, organization/team management, analytics, budgets, registration/verification, password recovery and logout. The browser/API workflow smoke covered publishing, activity, timer, callback, signal, pause/resume, output and audits. Helm lint passed for both database selections; Kubernetes deployment was not performed. Angular production build passed with existing stylesheet/initial-bundle size warnings. These checks do not certify high-load operation, paid model integrations, Google login, or distributed service extraction.
