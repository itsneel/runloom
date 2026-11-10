# Organization namespaces and run metadata

Updated: 2026-10-13. Audience: operators and backend developers.

## Behavior

Every organization uses one Temporal namespace per deployment environment. Teams and projects share their organization's namespace; their identities are searchable run metadata. All starts, status queries, signals, approvals, callbacks, resumes, cancellations, schedules and children use the organization-scoped client. Auth remains authoritative for organization/team membership; a request cannot choose a namespace or set trusted tags.

Namespace names are `<namespacePrefix>-<organization UUID>`. The prefix is an installation identity, not a display name. Keep it stable across restarts and upgrades, and use a different prefix for environments sharing a Temporal cluster. Organization renames do not rename namespaces. Temporal stores the organization UUID and deployment prefix in namespace ownership metadata; conflicting ownership fails closed. The mapping is deterministic, so no separate namespace registry database or migration is needed.

This release deliberately does not route old runs or schedules from the former shared namespace. Existing storage is not deleted. Recreate schedules and start new runs after switching. Changing the prefix later creates a different execution boundary and will make previous runs inaccessible through the new routing.

## Automatic setup

1. Workflows reads the authenticated Auth directory and registers each organization's namespace, including organizations without teams. A background reconciliation runs every 30 seconds; the first team request also ensures provisioning synchronously.
2. Workflows registers all required typed Search Attributes and verifies their types before admitting executions. Namespace propagation and concurrent registrations are retried within a bounded deadline. Failures return an unavailable response rather than starting an untagged run in `default`.
3. The worker discovers matching, fully provisioned namespaces every five seconds and starts an independent SDK worker for each namespace on the configured task queue. Existing namespaces are rediscovered on restart; creating an organization does not require a rollout.
4. Worker readiness fails when discovery cannot complete for a minute. Provisioning is idempotent across replicas. Namespace retention is assigned at creation and defaults to 30 days; changing the setting does not silently rewrite existing namespace retention.

The Workflows Temporal identity needs Describe/Register/ListNamespace, List/AddSearchAttributes and workflow/schedule operations. The worker identity needs ListNamespace/ListSearchAttributes, polling, execution and child-ancestry reads. Bundled local Temporal grants these automatically. Hosted self-managed Temporal must grant them explicitly. Temporal Cloud uses a separate namespace-management control plane; automatic Cloud provisioning is not implemented by this self-hosted provisioner.

Namespaces separate execution histories, task queues and visibility, but do not provide separate worker processes, database servers, encryption keys or CPU/memory quotas. All org pollers share the worker deployment. Trusted backend credentials can reach multiple org namespaces; application authorization remains mandatory. Restrict direct Temporal access to operators and service identities. For large organization counts, plan worker sharding and capacity before onboarding; no per-org autoscaler is included.

## Search Attributes

| Attribute | Type | Meaning |
| --- | --- | --- |
| `RunloomOrgId` | Keyword | Auth organization UUID |
| `RunloomTeamId` | Keyword | Authorized team UUID |
| `RunloomProjectId` | Keyword | Catalog project at admission; `default` is General |
| `RunloomDefinitionId` | Keyword | Workflow definition identifier |
| `RunloomDefinitionVersion` | Int | Published version, or zero for a draft test |
| `RunloomIsTest` | Bool | Studio tests and their nested children |
| `RunloomSource` | Keyword | `manual`, `consumer`, `webhook`, `schedule`, `child`, `draft_test` or `child_test` |
| `RunloomRootExecutionId` | Keyword | Root Temporal workflow ID; shared by descendants |
| `RunloomSnapshotId` | Keyword | SHA-256 of the normalized, pinned definition snapshot |

Root execution ID means Temporal Workflow ID, not Temporal Run ID. The latter is stored separately as `temporal_run_id`. Child metadata uses its own pinned definition version, project and snapshot, while retaining the parent's organization, team, root ID and test flag. Project metadata is an admission-time snapshot; later project moves do not rewrite history. Schedules pin their definition/project metadata when created and fill the actual root execution ID when each occurrence starts.

Temporal Search Attributes are plaintext and bypass payload encryption. Only the fixed allowlist above is indexed. Do not put personal or confidential information in definition identifiers or caller-supplied workflow IDs. Inputs, outputs, prompts, secrets and human email addresses are never copied into tags. Encrypted execution payloads remain unchanged. See [Temporal Search Attributes](https://docs.temporal.io/visibility#search-attributes) and [namespace management](https://docs.temporal.io/self-hosted-guide/namespaces).

For example, select an organization's namespace in Temporal UI and filter:

```sql
RunloomTeamId = '00000000-0000-4000-8000-000000000001'
AND RunloomDefinitionVersion = 2
AND RunloomIsTest = false
```

MongoDB and PostgreSQL run records retain the same immutable `metadata` object, returned as `run.metadata` by Studio history and as `state.metadata` in workflow status. PostgreSQL adds a JSONB column; MongoDB uses a nested document. Existing input/state/output encryption and atomic run/audit writes are retained. Database run history does not depend on Temporal visibility indexing or history retention. Consumer responses keep their restricted contract and do not expose internal metadata or other consumers' runs.

## Searching tags in Studio

Management → Run history shows project, version, source and test-run tags beside each run. Open run details to see organization, team, workflow, namespace, root execution and snapshot tags as well. These values are immutable execution metadata, not editable custom labels. Older runs without metadata show “Tags not recorded.”

1. Use **Search runs** for case-insensitive, literal substring matching on run IDs, workflow IDs and tag values. Inputs, outputs and execution-state payloads are never searched.
2. Click a tag, or choose **Tag** and enter **Exact tag value**, to restrict results to that case-sensitive value. Test tags use `true`/`false`; version `0` identifies a draft snapshot. Search and the exact tag filter combine with AND.
3. Search queries all stored history within the selected authorized team before returning the latest 200 matches. Choose **Load older matches** to fetch more. Workflow, status, run type, date filters, sorting and summary counts apply to the loaded matches, not all-time totals.
4. Use **Clear filters** to reset or **Refresh runs** to reload the newest matching records. Offset pages are not a frozen snapshot; concurrent new runs or deletions may shift pages. The dashboard deduplicates loaded run IDs.

The Studio `GET /api/v1/studio/runs` contract accepts `search`, `tag_key`, `tag_value`, `offset` and `limit`, documented in the interactive Studio OpenAPI specification. MongoDB and PostgreSQL filter metadata before limiting and decrypting matching records. The local filesystem adapter scans its stored run files. Search does not bypass team authorization or the configured payload-read policy. Consumer API visibility and responses are unchanged. This does not add arbitrary user-defined tags or migrate historical metadata.

Run `node scripts/test-run-tags.mjs` against the local MongoDB Compose stack, or set `RUNLOOM_UI_URL` to another local Studio address. It uses the public local-only admin defaults, creates a synthetic team and echo workflow, checks API filtering/team isolation and exercises dashboard tags and search in a browser. Synthetic records are retained. `TestRunTagSearch` covers filesystem, PostgreSQL and MongoDB filtering/paging beyond 200 runs; database variants require the opt-in test URLs described in [Contributing](../CONTRIBUTING.md).

## Configuration and verification

Workflows and worker must receive the same mounted-file settings:

```yaml
TEMPORAL_ADDRESS: temporal:7233
TEMPORAL_NAMESPACE: default # bootstrap client only; never the execution destination
TEMPORAL_NAMESPACE_PREFIX: runloom-local
TEMPORAL_RETENTION_DAYS: "30"
RUNLOOM_TASK_QUEUE: runloom
```

`make local-services-up SERVICES_DATABASE=mongodb` and its PostgreSQL variant generate the prefix from their Compose project. Helm sets `temporal.namespacePrefix` (default: release fullname) and `temporal.retentionDays`. Both become mounted configuration only in Workflows/worker; Auth, Management and UI need no Temporal configuration. The prefix accepts 1–63 lowercase DNS-style characters starting with a letter; retention accepts 1–3650 days.

```sh
make local-services-up SERVICES_DATABASE=mongodb
make test-services-live SERVICES_DATABASE=mongodb
make local-services-up SERVICES_DATABASE=postgres
make test-services-live SERVICES_DATABASE=postgres
make helm-lint
make k8s-upgrade
make test-k8s-services
```

`TestOrganizationNamespacesLive` creates two synthetic organizations after startup and checks automatic namespace setup, typed visibility filtering, cross-org rejection, persisted metadata, pinned child tags, draft tests, scheduled execution and replay of real encrypted histories. The existing live service suite also covers consumer idempotency/cancellation, callbacks, signals, pause/resume, accounting and encrypted persistence. Tests retain synthetic definitions/runs and delete their own schedules. Use only local test installations. Run history replay uses the real encrypted converter; namespace validation belongs in service/activity code because offline replay uses the SDK's synthetic namespace.

## Recorded local verification

Verified on 2026-10-13 using synthetic organizations and fresh teams, without migrating old executions:

| Deployment | Application database | Result |
| --- | --- | --- |
| Compose | MongoDB | Full live service suite, namespace/tagging checks and encrypted-history replay passed |
| Compose | PostgreSQL | Full live service suite, namespace/tagging checks and encrypted-history replay passed |
| Local kind Helm, `runloom` | MongoDB | Chart health test, browser/API smoke and namespace/tagging/replay checks passed |
| Local kind Helm, `runloom-pg` | PostgreSQL | Fresh installation, chart health test, browser/API smoke and namespace/tagging/replay checks passed |

Go unit tests, changed-package race tests, `go vet`, workflow determinism checks, independent-service offline build/tests, chart validation and local Kubernetes orchestration tests also passed. Helm verification did not exercise external Temporal, Temporal Cloud or production credential policies. Old MongoDB Helm team audit records caused budget-history reads to fail; the checks used fresh teams and did not enable legacy plaintext decoding or modify those records. Running all four installations simultaneously exceeded the local Docker VM's capacity, so live checks were sequenced.
