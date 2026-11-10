# Self-hosted version upgrades

Status: Draft. Updated: 2026-11-09. Audience: operators responsible for an existing self-hosted Runloom installation. Owner: TBD.

This guide describes how to prepare, apply and verify a newer Runloom release while preserving accounts, workflow definitions, published versions, runs and credentials. It covers the independent-service architecture and separates hosted Helm, operator-managed Docker Compose, and guarded local development helpers. It does not authorize a deployment or promise compatibility between arbitrary releases.

Implementation references were inspected on 2026-11-09 at revision `09e0aa1`. Commands below were reviewed against repository configuration; no upgrade, backup, restore or live workflow test was executed to validate this guide. Chart version, application version, image tag, source revision and workflow published version are different identifiers. The example tags in production values are placeholders, not a release catalogue.

## 1. Choose and assess the target release

1. Record the installed source revision, chart revision, actual image digests, database driver, public origin, enabled components, configuration files, Secret references and external dependency versions. Use a separate clean checkout of the target release; preserve local changes and the previous checkout. Do not update a running installation by pulling an unspecified branch tip.
2. Select an explicitly identified target release and read its release notes and migration instructions. Check every intervening release for required intermediate upgrades. A supported source-to-target version matrix is not established by this repository. If migration order, data compatibility or worker replay compatibility is unknown, stop and resolve those questions before production rollout; decision owner: the installation operator.
3. Treat a move from the retired monolithic backend to independent services as a separate migration. The service-split instructions describe fresh installation, not import of existing data. Do not apply this routine upgrade procedure to that transition. See [service ownership and migration boundaries](service-split.md) and [historical rename considerations](rebranding.md).
4. Rehearse the exact source-to-target transition using isolated copies of application databases, identity data, Temporal state and required encryption keys. Prevent the rehearsal environment from reaching production email, external APIs, model providers or production task queues. Replaying a workflow history and executing its activities are different operations; running activities may produce real side effects and costs.
5. Plan a maintenance window unless the release establishes mixed-version service and worker compatibility. Agree who can stop rollout, the restoration point, acceptable data loss and the verification needed to reopen access. No zero-downtime guarantee is implied by Kubernetes rolling updates.

## 2. Preserve state and recovery material

Back up before any hook, new binary or bootstrap tool can write to existing storage. Use your database and Temporal operators' tested backup procedures; this repository does not provide a complete cross-system backup/restore command. A volume's existence is not proof of a recoverable backup.

| Material | What must be preserved |
| --- | --- |
| Application storage | Auth, Workflows, Management and enabled Assistant databases; Workflows and worker share the Workflows database. Preserve any filesystem storage used by native installations. |
| Identity | Keycloak database, realm/client configuration, signing-key management and provisioner credential references. A realm JSON import is not a substitute for backing up identity data. |
| Temporal | Durable persistence, applicable visibility storage, namespaces and retention settings under the Temporal operator's restore procedure. Application database backups do not contain Temporal replay histories. |
| Encryption | Existing workflow-vault key, explicit execution-payload keyrings and every older key needed by retained histories, records or backups. Losing these keys can make restored data unreadable. |
| Deployment | Prior chart/source bundle, immutable image references, private values/configuration files, credential references, workload tokens and matching trust configuration, TLS materials and external-secret source records. |

1. Stop or gate new submissions and scheduled starts through your established maintenance controls. Account for consumer callers, callbacks, named signals, approvals and external integrations. There is no universal Runloom maintenance-mode command documented here. Do not claim a pause of one run prevents new runs or all external activity.
2. Decide whether existing runs will drain, remain waiting or continue under a verified worker compatibility plan. Graceful shutdown is bounded; activities can be retried after interruption. Confirm external operations are idempotent before allowing retries. Preserve pending timers, callbacks, approvals and signals by retaining the matching Temporal and application state.
3. Establish a recoverable point across application databases, identity, Temporal and keys. If snapshots are not coordinated, document the reconciliation needed on restore and test it. Keep backup contents private and encrypted, and verify restoration before relying on them.
4. Retain existing credentials and keys during a normal upgrade. Do not regenerate the workflow key, recreate a realm, rotate workload tokens, reset accounts or change database engines as an upgrade shortcut. Review [encryption compatibility and key retention](deployment.md#execution-security-configuration) and [workflow-vault backup requirements](api-nodes-and-secrets.md#operator-setup).

## 3. Prepare a consistent release bundle

1. Use the target release's own toolchain instructions. Source builds at the inspected revision use Node.js 26.13.0 via `.nvmrc`; Node is a build/development dependency, while deployed Studio images serve compiled assets through Nginx. Follow [contribution setup](../CONTRIBUTING.md) for Go and dependency preparation. `make setup-go` refreshes standalone vendor trees using local SDK sources and builds the services; ordinary Docker builds consume those prepared trees.
2. Build or obtain a coordinated set of images for Auth, Workflows, worker, Management, Gateway, the four Studio applications and enabled Assistant/documentation. Include the database-setup tool when its hook is enabled. Local development also uses the separate local-admin tool. Record immutable tags and resolved digests, and ensure cluster nodes can access the selected registry. See [image configuration](service-split.md#4-helm-and-externally-supplied-databases).
3. Review the target chart and configuration with the target binaries. Do not combine a new chart with stale `:local` images or independently update a federated Studio remote without verifying its shared platform/framework contract. Compare actual running `imageID` values after rollout; a matching tag alone does not establish matching image contents.
4. Merge existing operator settings into the target release's defaults deliberately. Preserve the database driver and names, Secret references, public login origin, workload trust, encryption keys, Temporal organization namespace prefix, task queues and TLS settings. Enabling Assistant or another component may require new resources; provision only those required by the release. Do not blindly replace private values with the production example or blindly reuse old defaults.
5. Check mounted YAML keys and credential paths against each target service's parser. Unknown keys cause startup failure; secret values must remain in credential files. Environment-only local behavior does not validate mounted YAML. For example, the development chart emits `RUNLOOM_LOCAL_ORGANIZATION_BOOTSTRAP_PENDING`; the Auth binary must accept that setting. See [Auth's allowlist](../server/auth/internal/config/file.go) and [service configuration rendering](../charts/runloom/templates/_service-config.tpl). Removing a required setting simply to bypass validation can produce a different startup failure.
6. Resolve release-specific schema/index changes before rollout. The database-setup hook provisions owner databases and access; it is not a general reversible data-migration framework. Some application storage initialization occurs at startup, so inspect the target release rather than assuming only an explicit migration command writes schema. See [database provisioning](../tools/database-setup/README.md) and [database behavior](database.md).
7. For releases changing worker code, require representative Temporal history replay checks and an explicit strategy for open runs. Immutable published workflow definitions do not version the interpreter. The chart does not automatically enable Temporal Worker Deployment Versioning. For the move to per-organization namespaces, old shared-namespace runs and schedules are not migrated; follow [Temporal organization compatibility](temporal-organizations.md) before proceeding.

Do not upgrade Keycloak, PostgreSQL, MongoDB or Temporal as an incidental Runloom change. If the selected release requires such a change, use that dependency's separately reviewed upgrade and restore procedure.

## 4. Hosted Kubernetes and Helm

Run from the reviewed target source checkout. Required access: the intended Kubernetes context/namespace, Helm release, registry, private operator values, backup systems and referenced credentials. Substitute the following placeholders before running commands. Keep these variables for the entire procedure; never rely on the default Kubernetes context.

```bash
RUNLOOM_CONTEXT='your-cluster-context'
RUNLOOM_NAMESPACE='your-namespace'
RUNLOOM_RELEASE='your-existing-release'
RUNLOOM_VALUES='/secure/path/reviewed-target-values.yaml'
RUNLOOM_UPGRADE_RECORD='/secure/path/upgrade-record'
umask 077
mkdir -p "$RUNLOOM_UPGRADE_RECORD"

helm history "$RUNLOOM_RELEASE" --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE"
helm get values "$RUNLOOM_RELEASE" --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" > "$RUNLOOM_UPGRADE_RECORD/previous-user-values.yaml"
kubectl --context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .status.containerStatuses[*]}{.name}{"="}{.imageID}{" "}{end}{"\n"}{end}' > "$RUNLOOM_UPGRADE_RECORD/previous-images.txt"
```

These are read-only cluster queries; the output files are private operator artifacts. Values files can contain sensitive information even when normal configuration uses Secret references. Do not paste them into tickets. Record the successful prior Helm revision and retain its chart and images separately; these queries do not back up application data or Secret contents.

1. Complete sections 1–3 and merge a full reviewed target values file. Do not pass `--reuse-values` as a substitute for reviewing changed defaults. The command below uses target defaults plus that file; omitted overrides can change behavior.
2. Validate and inspect rendered resources privately. Expected result: no lint/template failures; image references, namespaces, configuration keys, credential mounts, network settings and enabled components match the reviewed plan. Rendering does not check that an image understands its mounted keys or that credentials and external services work.

```bash
helm lint charts/runloom -f "$RUNLOOM_VALUES"
helm template "$RUNLOOM_RELEASE" charts/runloom --namespace "$RUNLOOM_NAMESPACE" -f "$RUNLOOM_VALUES" > "$RUNLOOM_UPGRADE_RECORD/target-manifests.yaml"
```

3. The next command changes the deployment and can run a database-setup `pre-upgrade` Job. Execute only after backups, prerequisites and release-specific migrations are ready. Use the existing release name; deliberately omit `--install` so a typo cannot create a second installation. `--reset-values` requires the complete reviewed overrides prepared above. The ten-minute timeout is an example operational budget, not a service target.

```bash
helm upgrade "$RUNLOOM_RELEASE" charts/runloom \
  --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" \
  --reset-values -f "$RUNLOOM_VALUES" --wait --timeout 10m
```

4. Stop on a failed hook, startup error or readiness failure. Do not repeatedly rerun bootstrap or delete persistent volume claims (PVCs) to force progress. Inspect resources and the specific failing pod with the same explicit context. Logs can contain protected operational information; retain them privately and redact before sharing.

```bash
helm status "$RUNLOOM_RELEASE" --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE"
kubectl --context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" get deployments,pods,jobs,pvc
# Replace the pod placeholder with the failing pod name:
kubectl --context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" describe pod '<pod-name>'
kubectl --context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" logs '<pod-name>' --previous --tail=100
```

5. Once ready, run the chart connectivity test and complete section 7. `helm test` creates the chart's test resources and performs connectivity checks; it is not a full authenticated workflow test.

```bash
helm test "$RUNLOOM_RELEASE" --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE"
```

## 5. Operator-managed Docker Compose

Use the same project name, ordered Compose files, private environment/configuration sources, volume names and credential mounts as the existing installation. The repository's bundled Compose infrastructure and local helpers are development-oriented; hosted Compose needs operator-managed production identity, Temporal, storage, networking and backups. This guide does not invent a universal production Compose override.

1. Record the exact invocation and existing container image IDs using `docker compose` with your existing arguments followed by `ps` and `images`. Review the target Compose files and validate using the same invocation followed by `config --quiet`. Do not print fully resolved configuration containing credentials into shared logs.
2. Prepare reviewed immutable image references and perform the required migrations before starting new writers. With your existing Compose arguments, use `pull` for published images, or `build` for reviewed source builds. Pull/build alone does not deploy. Retain the previous images and configuration.
3. The application-changing step is the existing invocation followed by `up -d`, using the target image/configuration bundle and the release's compatibility order. Do not add `--build` without intending a source build, and do not accidentally recreate external dependencies with changed versions. Inspect `ps` and the affected service logs; wait for health before restoring access.
4. Never use `down -v`, volume removal or a different Compose project name as an upgrade step. Those actions can destroy or detach the installation's data. Stopping containers preserves volumes but interrupts workers; apply the drain/retry plan first.

## 6. Guarded local development installations

These commands change an existing local installation and can build images, provision resources and ensure local accounts. They are not hosted production upgrade commands. Run from the target source checkout after completing the relevant backup and compatibility checks.

For an existing kind installation, repeat its original cluster, namespace and release settings:

```bash
make k8s-upgrade K8S_CLUSTER=runloom-local K8S_NAMESPACE=runloom K8S_RELEASE=runloom
make k8s-status K8S_CLUSTER=runloom-local K8S_NAMESPACE=runloom K8S_RELEASE=runloom
make k8s-test K8S_CLUSTER=runloom-local K8S_NAMESPACE=runloom K8S_RELEASE=runloom
```

These names are repository defaults, not an instruction to retarget a differently named installation. The helper rebuilds/loads images and preserves the installed database driver and login origin. Do not delete/recreate the kind cluster to repair a failed upgrade; deletion destroys cluster-local data. See [local Kubernetes lifecycle](deployment.md#development-cluster).

For the existing local Compose MongoDB installation:

```bash
make local-services-up SERVICES_DATABASE=mongodb
```

Use `SERVICES_DATABASE=postgres` only when that is the existing installation. Switching this selector chooses a separate dataset, not a migration. Preserve `~/.config/runloom/local/services/<compose-project>/`, its private credential/key files and the existing volumes. Re-running local setup reuses that installation; see [local preservation rules](service-split.md#3-fresh-local-setup).

## 7. Verify before reopening access

1. Confirm every intended workload is ready, restart counts are stable and running image digests match the release record. Check configuration errors, failed hooks, authentication failures and service-to-service failures rather than relying only on a green Studio/Nginx probe. A failed dependency can leave the gateway running but unready.
2. Verify login through the existing public origin, expected organization/team membership and role restrictions, Studio remote loading, and access to existing projects, drafts, published versions and run history. Confirm denied-access cases using approved test accounts. Do not grant new access to make a test pass.
3. Check Workflows and worker health, Temporal namespace/task-queue discovery, and representative pre-upgrade running/waiting executions under the release's compatibility plan. Do not cancel, signal, approve or resume a production run solely for verification without its owner's authorization.
4. With synthetic data and approved integrations, save a draft, publish an immutable version and start a controlled run. Test the applicable timer, callback, named-signal, approval and consumer paths separately. These actions write records and may send real email, call APIs or incur model costs; use a disposable workflow and sandbox destinations. Repository smoke tests also create records and may start/cancel runs; follow [authentication test prerequisites](authentication.md#validation).
5. Verify existing encrypted credentials and execution payloads remain readable without logging them. Check Management usage/budget behavior and enabled Assistant/documentation. Report unknown usage or cost as unknown, not zero. Observe through the agreed window and record results, image digests and the completed release revision. Only then reopen normal access and scheduled starts.

## 8. Rollback and recovery

1. Stop promotion on configuration rejection, unreadable encrypted data, failed migrations, replay errors, broken authentication, unexpected access or sustained failed runs. Keep maintenance controls in place and preserve diagnostics. Do not rotate keys, reset accounts or remove storage as a rollback technique.
2. Determine whether old binaries can read data written since upgrade and replay histories advanced by new workers. A successful Helm rollback or container replacement does not undo database/index changes, Keycloak changes, external activity side effects or Temporal history. If compatibility is unproven, use the tested restore or forward-fix plan instead of blindly starting old writers.
3. When application and history compatibility are confirmed, select the recorded prior Helm revision explicitly. This command changes deployed resources; it is not a database restore:

```bash
RUNLOOM_PREVIOUS_REVISION='replace-with-recorded-successful-revision'
helm rollback "$RUNLOOM_RELEASE" "$RUNLOOM_PREVIOUS_REVISION" \
  --kube-context "$RUNLOOM_CONTEXT" --namespace "$RUNLOOM_NAMESPACE" \
  --wait --timeout 10m
```

4. For Compose, restore the recorded prior image references and compatible configuration with the same project/files/volumes, then apply using the established invocation. For local kind, an older chart revision may still reference a replaced local image tag; verify the old image contents are available before relying on Helm history.
5. If restoring state is required, stop incompatible writers and restore the coordinated application, identity, Temporal and key recovery set using the tested operator procedure. Account explicitly for submissions and external effects after the backup point; database restoration cannot retract an email or API transaction. Repeat section 7 before reopening access.

## 9. References and revision history

The authoritative setup and preservation references are [deployment](deployment.md), [chart configuration](../charts/runloom/README.md), [service ownership](service-split.md), [workflow definition versus worker versioning](versioning.md), and [organization namespace compatibility](temporal-organizations.md). Relevant implementation entry points are the [Makefile](../Makefile), [local Compose helper](../scripts/local-services.mjs), [local kind helper](../scripts/local-k8s.mjs), [database-setup hook](../charts/runloom/templates/database-setup.yaml), and [Auth storage initialization](../server/auth/internal/recordset/recordset.go).

2026-11-09: Initial draft covering independent-service self-hosted upgrades, state preservation, configuration/image alignment, verification and compatibility-limited rollback. No supported release-transition matrix, complete backup tooling or migration automation is claimed.
