# Application database

The Go service supports PostgreSQL, MongoDB, and a filesystem development backend. Select the backend with `RUNLOOM_DATABASE_DRIVER`. Database initialization is idempotent and runs when API and worker open their stores. Both processes must use the same connection settings and, for filesystem mode, the same data directory.

Schedule configuration is durable Temporal state, not an application database table. Scheduled and child executions register original input and start audits through a worker activity before business steps run. Their `trigger_type` values are `schedule` and `child`; run state includes schedule and parent/child links. See [schedules and subworkflows](schedules-and-subworkflows.md).

Team-scoped project folders and workflow membership use a revision-controlled project catalog in the same selected backend. Project metadata is separate from immutable workflow definitions and run history. See [project storage and API semantics](projects.md).

## Stored data

Prompt-library storage has its own `Prompts` port and `prompts` persistence scope, separate from workflow and project operations. Drafts, immutable published versions and revision history are separately keyed records. Revision compare-and-swap atomically writes the draft, publication and attributed history; stale edits return a conflict. PostgreSQL and MongoDB reuse the normalized record repository and team isolation; filesystem development stores `_prompts/library.json`. MongoDB requires the same replica-set transaction support as the other revisioned modules. Published workflows embed a template snapshot and hash, so workers never fetch mutable library content during execution. Library capacity is 200 prompts and 10000 revisions per team; history retrieval is bounded by that retention cap, not cursor-paginated.

1. Definitions contain the editable version `0` draft and immutable numbered published versions.
2. Definition commits contain the immutable JSON snapshot, per-definition sequence, kind, actor, message, optional published version or restore source, and timestamp.
3. Workflow runs contain the Temporal workflow and run identifiers, exact definition version, trigger type, original JSON input, Temporal status, latest query-state snapshot, resolved output, and lifecycle timestamps.
4. Audit events contain a unique event identifier, optional workflow and definition identifiers, event type, optional step identifier, actor, JSON payload, and timestamp.

## PostgreSQL

The API creates `runloom_definitions`, `runloom_definition_commits`, `runloom_runs`, and `runloom_audit_events`, plus indexes for definition and workflow history queries. JSON values use `JSONB`. Saving, publishing, and restoring serialize changes per definition and commit their related records in one transaction.

```bash
export RUNLOOM_DATABASE_DRIVER=postgres
export RUNLOOM_DATABASE_URL=postgresql://runloom:runloom@localhost:5432/runloom
```

## MongoDB

The API creates `definitions`, `definition_commits`, `definition_counters`, `workflow_runs`, and `audit_events` collections. Unique indexes protect definition versions, commit sequences, workflow identifiers, and audit identifiers. Compound indexes support definition, recent-run, and audit-timeline queries.

MongoDB must be a replica set or a transaction-capable sharded deployment. Standalone MongoDB is no longer supported. The bundled Compose and development Helm configurations initialize a single-member `runloom` replica set; this is a local/development topology, not high availability. Definition snapshots and commit history use transactions, including rollback if either write fails.

```bash
export RUNLOOM_DATABASE_DRIVER=mongodb
export RUNLOOM_DATABASE_URL=mongodb://localhost:27017/runloom
export RUNLOOM_DATABASE_NAME=runloom
```

## Filesystem development mode

Set `RUNLOOM_DATABASE_DRIVER=file` to retain the lightweight local store. Definitions, `_runs`, and `_audits` are written below `RUNLOOM_DATA_DIR` with atomic file replacement. This backend is not intended for multiple API replicas.

## Audit semantics

1. Definition save and publish actions record the requesting actor from `X-Runloom-Actor`, falling back to `api-user`.
2. Workflow start records the trigger and immutable definition version. The complete input remains on the run record.
3. Approval, callback, named signal, resume, and cancellation actions record the authenticated user subject and payload.
4. A background reconciler and live run reads update the database snapshot only when status or state changes, preventing duplicate polling events.
5. Application audit data complements Temporal Event History. It is not used during replay and cannot change workflow behavior.
6. The authentication gateway overwrites supplied actor headers/fields with the verified user subject. Organization/team membership changes have a separate organization-scoped audit endpoint.

Team data is isolated using one PostgreSQL schema, MongoDB collection prefix, or filesystem directory per team. Organization membership data uses a revision-controlled directory in the selected store. Existing unscoped records are preserved but not automatically assigned to a team. Passwords and account-recovery state live only in Keycloak's separate database. See [accounts and isolation](authentication.md).
7. Audit rows are write-once through these APIs. With execution encryption enabled, authenticated payload encryption binds each payload to its event identifier, actor, type and resource identifiers. This detects those fields being altered or substituted; it does not detect whole-record deletion, timestamp changes or database rollback and is not an immutable audit archive or compliance certification.
8. Temporal messages and database audit writes are separate operations. Production integrations should use stable idempotency keys and an outbox or equivalent reconciliation design when exactly-once business recording is required.

## Production controls

Inputs, outputs, callbacks, signals, and audit payloads may contain sensitive data. Production deployments should define field-level redaction, retention and deletion rules, database encryption, least-privilege credentials, access logging, tenant isolation, backups, and restoration tests. Large payloads should move to encrypted object storage with references stored in Temporal and the application database.

## Stage 1 execution security

Implementation and verification date: 2026-10-13. These controls support security requirements; they are not a HIPAA/GDPR compliance determination or SOC 2 attestation.

1. PostgreSQL and MongoDB encrypt new run inputs, state snapshots, outputs, workflow audit payloads and consumer admission/retry records using AES-256-GCM. Run fields are authenticated against their run identifier and field name. Database indexes and routing identifiers remain readable. This is application-level encryption, separate from database-volume encryption.
2. Workflows API and worker use the same Temporal payload codec, including signal, activity and workflow payloads, schedule memo and encoded failure attributes. Ordinary Temporal Web/CLI views cannot decrypt these payloads. No publicly accessible codec/decryption endpoint is added.
3. `RecordRunStart` and accepted run observations atomically commit their metadata-only `run.recorded`/`run.observed` audits with the run write. MongoDB uses snapshot transactions and majority write concern. Audit-write failure rolls back that database change.
4. Authenticated workflow-service Studio operations and authorized consumer operations record request intent before business work and record an HTTP outcome before releasing the response. Receipts use `X-Runloom-Request-ID`. Intent without completion means **unknown outcome**, not success or failure. A completion-write failure returns 503 even if the operation happened. Consumer starts must retry with the same idempotency key; Studio starts still have no automatic retry guarantee. Temporal and the application database remain separate systems, not an exactly-once transaction.
5. Detailed execution, audit and schedule reads default to team admins, including effective admins through organization roles. Authoring alone does not grant access to production payloads. Operators can explicitly extend `RUNLOOM_SENSITIVE_RUN_ROLES` to a comma-separated subset of `admin,editor,viewer`. Consumer visibility remains governed by its existing run ownership, scopes and field allowlists. This is a coarse installation-level control, not the future per-field/per-project permission model.
6. Workflow identifiers, definition identifiers, audit actors and audit timestamps remain metadata. Authoring definitions, commit messages, the Management prompt library and identity records are not covered by this execution-data encryption layer. Do not embed personal data or credentials in those fields; use runtime inputs and secret references. Filesystem development storage is not covered by PostgreSQL/MongoDB run encryption.

See [deployment and key lifecycle](deployment.md#execution-security-configuration) for rollout, rotation and compatibility requirements. Retention, data-subject requests, per-organisation keys, signed audit batches, immutable archive export, emergency-access approval and automatic workload-token rotation remain separate work. Task-queue fairness is also recommended for tenant resource isolation, but is not implemented by this security change.

Verification uses `make test-security-live`, which starts separate local MongoDB and PostgreSQL Compose installations and runs browser/API/worker tests against both. It creates synthetic teams and workflows; database fault-injection tests create and remove only their uniquely named team schemas/collections. Tests inspect raw stored ciphertext and Temporal payloads, replay encrypted histories, check role/tenant boundaries and authentication-strength claims, and force audit failures to verify rollback. Unit tests additionally cover tampering, missing keys, key rotation and prohibited plaintext downgrade. Hosted TLS/identity-provider enrollment and infrastructure backup protection require their own deployment verification.

## Fresh-install storage layout

The 2026-10-02 refactor intentionally targets a fresh application database. It does not migrate the old access/project/consumer blob documents, nor add the new observation column to an existing runs table. Use a new database/Compose project; do not point the new binaries at an old installation and assume compatibility. See [isolated fresh startup](local-development.md#fresh-install-verification-stack).

1. PostgreSQL `runloom_entity_heads` tracks revisions by owner scope; `runloom_entity_records` stores separately keyed entities as JSONB. Global identity, secrets, credential and admission scopes use the application schema; team metadata lives inside the team's schema. No cross-owner transaction or foreign key is required.
2. MongoDB uses `runloom_identity_entities`, `runloom_secrets_entities`, `runloom_consumers_entities`, and `runloom_admission_entities`, plus each team's prefixed `metadata_entities`. Entities and audit/history entries are separate documents, updated with snapshot transactions and majority writes for metadata.
3. Team metadata has separate `projects`, `leases`, and `policies` revision scopes. Lease renewal no longer shares the project/policy revision. The filesystem equivalent is `_projects/projects.json`, `leases.json`, and `policies.json`; account files are `_identity.json`, `_secrets.json`, and `_consumers.json`.
4. Consumer admission normalizes each run entry on database backends; rate counters use a separate scope. Idempotency records have no 10,000-run lifetime cutoff. Admission still scans the account's aggregate, and file mode retains its 8 MiB document cap; neither should be advertised as unlimited throughput.
5. Run projections carry an observation watermark. Older observations cannot overwrite newer ones, terminal statuses cannot regress, and unavailable query state preserves the last recorded output. Reconciliation uses cursor pages of unfinished runs instead of the newest 200 records.
6. Monthly budget totals use database aggregation over direct attempts, preserving decimal arithmetic and incomplete-accounting flags. Analytics reports retain their separately documented limits. Database setup still runs on store opening; a dedicated migration/provisioning workflow remains outstanding.
