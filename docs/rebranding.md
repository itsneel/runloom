# Runloom naming and migration

Runloom is the new name for the project formerly called JSON Flow / Temporal JSON Workflows. This is a complete naming cutover for new installations, not an automatic upgrade of an existing deployment. The MIT terms and contributor ownership are unchanged; all project license copies use the new collective project name.

## Naming contract

| Area | Previous name | Runloom name |
| --- | --- | --- |
| Repository folder | `temporal-json-workflows` | `runloom` |
| Go module | `github.com/example/temporal-json-workflows-go` | `github.com/example/runloom/server` |
| Studio package / Angular project | `workflow-studio` | `runloom-studio` |
| Website package / Angular project | `jsonflow-website` | `runloom-website` |
| Helm chart path | `charts/jsonflow` | `charts/runloom` |
| Default Compose project | `jsonflow-go` | `runloom` |
| Backend / UI images | `temporal-json-workflows-go`, `temporal-json-workflows-ui` | `runloom-backend`, `runloom-ui` |
| Backend image override | `GO_IMAGE` | `BACKEND_IMAGE` |
| Executables | `jsonflow-api`, `jsonflow-worker`, `jsonflow-healthcheck` | `runloom-api`, `runloom-worker`, `runloom-healthcheck` |
| Application configuration | `JSONFLOW_*` | `RUNLOOM_*` |
| HTTP headers | `X-Jsonflow-*` | `X-Runloom-*` |
| Temporal workflow type | `JSONWorkflow` | `RunloomWorkflow` |
| Default task queue | `jsonflow-go` | `runloom` |
| Built-in activity names | `jsonflow.echo`, `jsonflow.compose`, `jsonflow.sum`, `jsonflow.llm` | `runloom.echo`, `runloom.compose`, `runloom.sum`, `runloom.llm` |
| Keycloak realm / client | `jsonflow` | `runloom` |
| Realm import filename | `jsonflow-realm.json` | `runloom-realm.json` |
| Default application database / local PostgreSQL role | `jsonflow` | `runloom` |
| Application storage prefix | `jsonflow_` | `runloom_` |
| Browser organization/team selection keys | `jsonflow-org`, `jsonflow-team` | `runloom-org`, `runloom-team` |

The Go module still uses the existing placeholder GitHub owner `example`; no remote repository has been created or renamed. `TEMPORAL_ADDRESS` and `TEMPORAL_NAMESPACE` retain their vendor-specific names. Go-language Make targets such as `make test-go` remain valid and point at `server/`.

## New installations

Use the current README and environment examples. Compose creates separately named `runloom_*` volumes; existing volumes are untouched. The native API defaults to `var/runloom/definitions` and the documented native Temporal database is `var/runloom/temporal.db`, relative to the command's working directory. Old environment prefixes and HTTP headers have no compatibility aliases.

The website remains an independent static Angular build, with no Docker or Helm deployment. Its output is `Website/dist/runloom-website/browser`. Studio has three independent project outputs: `studio/projects/host/dist/browser`, `studio/projects/workflows/dist/browser` and `studio/projects/management/dist/browser`; see [Studio build instructions](../studio/README.md#production-build).

## Existing installations

1. Back up the application databases, identity database, Temporal persistence, filesystem records, and deployment configuration before planning a migration. Keep the prior images and configuration available for rollback. Do not run volume deletion commands or point new defaults at old data assuming automatic migration.
2. Inventory open Temporal executions. Keep the old API and workers on their original queue until those runs finish, or carry out a separately designed and replay-tested migration. New workers register `RunloomWorkflow` and `runloom.*` activities only. Never rewrite Temporal histories or immutable published snapshots to rename activity commands.
3. Start Runloom with isolated storage and its new task queue. Import copies of editable definitions, replace built-in activity identifiers in those copies, validate, and publish new versions. Historical run records, published versions, and commit history are not automatically transferred.
4. For a data-preserving upgrade, explicitly migrate PostgreSQL table/index and team-schema prefixes, MongoDB access/team-collection prefixes, memberships, and stored definitions in a backup-tested maintenance procedure. Unprefixed MongoDB collection names remain unchanged. Preserve user and team identifiers. No data migration script is supplied by this rename.
5. Configure the new identity realm/client and public URL together. A realm name change changes token issuer and audience expectations; merely changing environment variables does not migrate users. Reconfigure Google redirect URLs if applicable, preserve identity mappings through a planned identity migration, and sign in again. Startup realm import does not rename an existing realm.
6. Update API clients to `X-Runloom-*`, deployment secrets/configuration to `RUNLOOM_*`, image references, monitoring, and release names. The Helm chart uses new labels, resource names, and PVC names; do not assume an in-place Helm upgrade retains old resources or their data. Test a separate release first.
7. Verify login, tenant isolation, version history, workflow execution, cancellation, callbacks, and audits against both old and new installations before retiring the old deployment. The rename itself makes no changes to running services or persisted data.
