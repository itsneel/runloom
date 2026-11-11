# Runloom Helm chart

Use the [Helm values guide](../../docs/helm-values.md) to choose a deployment mode, prepare required resources, and configure optional features.

## Documentation website

The independent `docs` Deployment and ClusterIP Service serve the documentation site on a separate browser origin. `docs.publicURL` defaults to http://localhost:4400 and is mounted into Studio's public configuration; the sidebar opens it in a new tab. Docs receive `auth.publicURL` as their return-to-Studio destination. Neither public file includes credentials. `make k8s-docs` forwards the service locally, and `make k8s-open` forwards docs together with Studio. `K8S_DOCS_PORT=4500 make k8s-upgrade` changes the installed local docs address and sidebar link together.

For hosted installations, publish the independent image with `make docker-build-docs` and configure a separate DNS record and certificate. Example values:

```yaml
docs:
  publicURL: https://docs.example.com
  image:
    repository: registry.example.com/runloom-docs
    tag: your-release
  ingress:
    enabled: true
    className: nginx
    tls:
      - secretName: docs-tls
        hosts: [docs.example.com]
```

The chart derives the docs ingress hostname from `docs.publicURL`; it does not add a `/docs` path to Studio. Ingress controllers and certificate provisioning remain operator-managed. To use an external static host, set `docs.enabled=false` while retaining its separate `docs.publicURL`. Documentation has no database, service tokens or backend runtime dependency. Its HTTP health check participates in `helm test` when enabled. Native builds and the non-containerized marketing website remain independent.

Updated: 2026-10-15. Chart version 0.4.0 targets the independent-service architecture. Local Make commands bootstrap the dependencies before installing this chart. Direct Helm and hosted deployments retain operator-managed database and credential references.

Ingress remains optional. See [optional ingress setup](../runloom-ingress/README.md) for the separate NGINX controller release, existing-controller mode and externally managed routing. The local hostname examples use `studio.runloom.dev` with HTTPS; default port-forward access remains `http://localhost:4200`.

## Mounted backend configuration

Workflows automatically registers a Temporal namespace and typed run attributes for every organization; workers discover them without restarting. `temporal.namespacePrefix` defaults to the release fullname and must remain stable. Use a unique prefix for each environment sharing Temporal. `temporal.retentionDays` defaults to 30 and applies when a namespace is created. `temporal.namespace` is only the bootstrap client namespace. Hosted self-managed Temporal must grant namespace/search-attribute provisioning permissions. See [organization namespaces](../../docs/temporal-organizations.md). Old shared-namespace runs and schedules are not migrated.

`make test-k8s-services` checks browser login, consumer execution and automatic multi-org namespace/tagging behavior against the selected local kind release. It uses temporary port-forwards and a private replay-key file, then removes both. Stop any existing Studio port-forward first. To check a separate PostgreSQL installation, use `make k8s-up K8S_RELEASE=runloom-pg K8S_NAMESPACE=runloom-pg K8S_DATABASE=postgres K8S_PORT=4400`, then `make test-k8s-services K8S_RELEASE=runloom-pg K8S_NAMESPACE=runloom-pg`. Synthetic test records are retained.

The chart renders separate `api.yaml` (gateway), `auth.yaml`, `workflows.yaml`, `management.yaml` and `worker.yaml` entries in the release ConfigMap. Each backend mounts only its entry read-only at `/etc/runloom/config.yaml`, selected by `RUNLOOM_CONFIG_FILE`. Auth, Workflows and Management mount their own database URL and workload token; the worker shares only the Workflows database and has its own workload token. The gateway has no database or credential mounts. No secret values belong in configuration values or ConfigMaps. A configuration checksum triggers rollouts when settings change.

Application Deployments include Angular host, workflows UI, management UI, gateway, Auth, Workflow API, Management, worker and the optional Assistant. Each has its own independently built runtime image. Database setup and local administrator setup use separate short-lived tool images. The gateway is built from `server/gateway/`. Development mode adds persistent Temporal, Keycloak with its own PostgreSQL database, and Mailpit. The guarded local helper provisions the application database server and credentials; the database-setup hook provisions database owners when enabled.

## Automatic local installation

Install Docker, kind, kubectl, Helm and the repository's supported Node.js version, then run:

```bash
make k8s-up
make k8s-open
```

Local startup prints one account table for the four Runloom accounts and the bundled Keycloak administrator (`admin` / `development-only`). It also prints the Keycloak console URL at the Studio origin, `/auth/admin/master/console/`. Sign in to `master`, then select `runloom`. Displayed passwords are local defaults; existing passwords are preserved. Runloom's explicit account-reset command does not reset Keycloak's administrator password.

No `K8S_SERVICE_VALUES` file is required. The default is MongoDB; use `make k8s-up K8S_DATABASE=postgres` for a fresh PostgreSQL installation. The helper creates/reuses the local kind cluster, builds and loads the independent application images plus dedicated database-setup and local-admin tools, starts an authenticated database server, and creates the credential references. Helm then runs the chart's `database-setup.yaml` pre-install/pre-upgrade Job to create independently owned databases and verify denied cross-database access before deploying the applications. A failed Job blocks the install/upgrade; application servers never receive database-administrator credentials.

The helper creates workload-token Secrets and a trust-hash ConfigMap, configures the Keycloak service-account provisioner and creates four local-only accounts. Organization `Runloom`, team `General` receive `admin@runloom.test` / `RunloomLocal!123` (owner/admin) and `member@runloom.test` / `RunloomMember!123` (member/editor). Separate accounts `platform-admin@runloom.test` / `RunloomPlatformAdmin!123` and `platform-staff@runloom.test` / `RunloomPlatformStaff!123` access `/platform`, with no tenant memberships added. Platform staff are restricted to service health and organization metadata. `make k8s-upgrade` rebuilds and verifies the same setup without resetting data, passwords, member roles or platform access decisions. `make k8s-open` ensures all four accounts using the dedicated `images.localAdmin` image, prints connection and login information including actual platform roles, and starts the product website linked to Studio. Revoked platform access stays revoked on repeated setup. Upgrade older installations with `make k8s-upgrade` first; Auth no longer contains the setup executable. These public accounts are created only by the guarded local helpers, never by a normal hosted Helm installation. Default Studio origin: http://localhost:4200. Default website: http://127.0.0.1:4300.

The bundled local database StatefulSet is managed by `scripts/local-k8s-services.mjs`, before Helm runs its database-setup hook. MongoDB waits for its headless Service name to resolve to the current pod IP on every container start. Its five-minute startup probe initializes only a genuinely uninitialized replica set (`NotYetInitialized`), then waits for the `runloom` primary. Readiness is read-only; it never reinitializes or force-reconfigures existing storage. Persistent startup failure restarts the container; TCP liveness does not treat a temporary loss of primary as a reason to kill a running database. Existing PVC names, replica-set membership and credentials are retained. Hosted MongoDB lifecycle remains the operator's responsibility.

If an older local installation is stuck with `InvalidReplicaSetConfig` after a cluster/DNS outage, first confirm the Service resolves to that database pod and inspect the replica-set configuration. Restart only that pod with `kubectl --context kind-runloom-local -n runloom delete pod runloom-database-0`; the StatefulSet reattaches its existing PVC. Never delete the PVC or regenerate credentials as a readiness fix. Apply the updated helper through `make k8s-upgrade` to retain the startup protection on future restarts. This is a single-node development recovery path, not a production replica-set recovery procedure. See [Kubernetes startup probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/) and [MongoDB initialization](https://www.mongodb.com/docs/manual/reference/method/rs.initiate/).

Verified 2026-10-14 on `kind-runloom-local/runloom`: restarting and applying the updated database helper retained the original bound PVC and three service databases, restored the writable primary, and left all application pods Ready. Helm connectivity and the browser/service smoke passed, including login, secret storage, prompt publishing, workflow activity/timer/callback/signal/pause-resume, audits and consumer idempotency/cancellation/revocation. Probe regression tests cover fresh initialization, invalid existing configuration, authentication failure, the temporary standalone bootstrap server and stale/missing DNS. The separate PostgreSQL installation remained Ready and its rendered configuration is unchanged; its full browser suite was not rerun for this MongoDB-only fix. Synthetic smoke-test records remain for inspection.

Local verification on 2026-10-12 covered a fresh MongoDB installation, browser login, an upgrade preserving all credential/vault bytes, and PostgreSQL provisioning plus live workflow/consumer API tests in a disposable namespace. Administrator bootstrap waits for Auth/Keycloak rollouts and has bounded retries for transient startup failures. Hosted deployment and external identity providers were not exercised by those checks.

The bootstrap database StatefulSet, PVC, immutable credentials and trust ConfigMap are managed by the local helper outside Helm. Helm uninstall does not delete them. Never remove their credentials while retaining the data; setup refuses to invent replacement credentials for an existing database. The local database uses the cluster's default StorageClass and a 5Gi PVC. Fresh namespaces are independent installations. `K8S_CLUSTER`, `K8S_NAMESPACE` and `K8S_RELEASE` select the target; all commands validate the explicit kind context and loopback API endpoint. Normal startup and upgrade never delete data.

## Operator-managed installation

### Database provisioning hook

The same setup Job is available to direct Helm installs. The database server must already be reachable, and credential Secrets must exist before Helm starts, because pre-install hooks run before ordinary chart resources. For local Make commands these prerequisites are automatic. For externally managed installations, either provision the databases independently and leave the hook disabled, or configure:

```yaml
databaseSetup:
  enabled: true
  existingSecret: runloom-database-bootstrap
images:
  database-setup:
    repository: registry.example.com/runloom-database-setup
    tag: YOUR_RELEASE_TAG
```

The bootstrap Secret contains `database-admin-url`, `auth-database-password`, `workflows-database-password` and `management-database-password`. Owner passwords must be at least 32 characters. Each owner's configured database URL Secret must contain the matching credentials; those URLs are projected into the Job from the same Secrets used by the servers. This tool provisions only the names `runloom_auth`, `runloom_workflows` and `runloom_management`. Bootstrap credentials are mounted only in this short-lived Job, never in application Deployments. Kubernetes Secrets may be created by an external secrets controller, but must be ready before installation. Do not put credential values in Helm values.

The Job retries transient failures twice, has a five-minute deadline and retains its result for one day (or until the next install/upgrade) for diagnosis. It is idempotent: rerunning setup does not erase data or reset existing passwords. Inspect it with `kubectl --context kind-runloom-local -n runloom get job runloom-database-setup` and `kubectl --context kind-runloom-local -n runloom logs job/runloom-database-setup`. See [Helm hook lifecycle](https://helm.sh/docs/topics/charts_hooks/) for ordering and failure behavior. This is installation-time server bootstrap, not provisioning on every server replica restart.

### Required external resources

Assistant is enabled by default. In addition to the three core databases below, provision `runloom_assistant` with its own owner and supply `assistant.databaseSecret` (URL key defaults to `url`). If the database-setup Job is enabled, this Secret also needs the owner `password` key. The assistant pod receives only the URL. Configure its model through `assistant.provider/baseURL/model/apiKeySecret`, or leave the provider disabled while retaining history. See [Assistant configuration](../../docs/assistant.md). Set `assistant.enabled=false` to omit the service.

These prerequisites apply to direct Helm/hosted deployments, or when explicitly supplying `K8S_SERVICE_VALUES` to opt out of automatic local database bootstrap:

1. Provision three databases and owner-restricted users: `runloom_auth`, `runloom_workflows`, `runloom_management`. Set `database.driver` to `postgres` or `mongodb`. MongoDB requires authentication and a transaction-capable replica set. Verify each user cannot read or write the other service databases. No migration from monolith storage is performed.
2. Create three separate Kubernetes Secrets containing their connection URLs, using `splitServices.databases.<owner>.existingSecret` and `.secretKey`. The database selected in the URL must agree with `.name`. Databases must be reachable from the cluster; `localhost` in a pod does not refer to your laptop.
3. Create four distinct service-token Secrets for `auth`, `workflows`, `management` and `worker`. Each contains a `token` key with at least 32 random characters. Create the `splitServices.trustConfigMap` ConfigMap with a `trust.json` entry mapping each workload name to the SHA-256 hex digest of its exact token. Secrets may be supplied by your external secret controller; the chart accepts existing references, never credential values.
4. Build and publish/load the independently configured images: `runloom-auth`, `runloom-workflows`, `runloom-worker`, `runloom-management`, `runloom-gateway`, enabled `runloom-assistant`, and the four Studio images `runloom-ui`, `runloom-workflows-ui`, `runloom-management-ui` and `runloom-platform-ui`. Include `runloom-docs` when documentation is enabled and `runloom-database-setup` when bootstrap is enabled. Workflows API and worker build separately from the self-contained `server/workflows` context using `Dockerfile` and `Dockerfile.worker`; neither depends on the other's image. Configure the matching `images` entries and the separate `docs.image` values. Guarded local account setup also uses the independent `images.localAdmin` image; hosted deployments do not create default accounts.
5. Configure a Keycloak provisioner client and set `consumer.provisioning.enabled`, `.adminURL`, `.clientID`, `.existingSecret` and `.secretKey`. Auth requires this client for the native Keycloak organization directory as well as consumer service accounts. Enable Organizations in the realm and grant the permissions described in [authentication setup](../../docs/authentication.md). Its credential is mounted only into Auth; the explicit local administrator Job also mounts it during guarded local setup. Raw Helm installation does not create that Keycloak client or seed a Studio administrator; the guarded local helpers perform those identity setup steps automatically.
6. Save the references and desired settings in a private `service-values.yaml`. Hosted deployments also require externally managed Temporal and Keycloak; start with `values-production.yaml`. Never put database passwords or token values in values files.

For a local kind cluster using separately provisioned resources:

```bash
make k8s-up K8S_SERVICE_VALUES=/absolute/path/to/service-values.yaml
make k8s-open
```

The helper builds and loads images using independent Docker contexts and uses the explicit `kind-runloom-local` context. Preserve the configured browser origin on upgrades because Keycloak retains redirect URLs. The chart requires Kubernetes 1.26+, Helm 3 or 4, and a StorageClass for bundled development dependencies. The public local administrator is automatically created only for the managed local database topology; an external database needs operator-managed identity/directory setup.

## Upgrades from the previous chart

Chart 0.2.0 removes `images.backend` and `database.name/existingSecret/secretKey`; use the service-specific references instead. It also removes the unused shared application database Secret, Service and StatefulSet. An upgrade removes those Kubernetes objects; the old database PVC is retained, but its server stops. Back up any old data and provision separate databases before upgrading. Do not apply this as an automatic monolith data migration.

`make k8s-upgrade` preserves existing split-service settings in a private temporary file, removes obsolete monolith defaults and uses the new chart defaults. Supply `K8S_SERVICE_VALUES` only when using operator-managed resources. The helper rejects monolith releases before building images. For a direct Helm upgrade, prepare a complete updated values file and use `--reset-values -f service-values.yaml`; do not carry obsolete values with `--reuse-values`. Preserve vault keys, identity configuration and workload credentials. Staging deployment remains a separate, explicitly authorized process.

## Key values

The Helm connectivity test runs `/runloom-gateway healthcheck <UI health URL>`. The subcommand exits before application configuration, telemetry or database initialization. It preserves the former probe's four-second HTTP timeout and exit codes (0 for HTTP 200, 1 for HTTP/transport failure, 2 for an invalid argument count). Normal server health endpoints and Kubernetes HTTP startup/readiness/liveness probes are unchanged. Upgrade the chart and images together: new images no longer contain `/runloom-healthcheck`, and older images do not support the subcommand. Compose uses the matching service executable with the same readiness URLs and timing settings.

| Value | Purpose |
| --- | --- |
| `images.auth.repository/tag`, `images.workflows.repository/tag`, `images.management.repository/tag` | Independent service images |
| `images.worker.repository/tag` | Separate worker runtime image, built with `server/workflows/Dockerfile.worker` |
| `images.gateway.repository/tag` | Stateless public gateway image |
| `images.ui.repository/tag` | Authentication host image |
| `images.workflows-ui.repository/tag` | Independent workflow application image |
| `images.management-ui.repository/tag` | Independent management application image |
| `imagePullSecrets` | Existing registry Secret references |
| `images.localAdmin.repository/tag/pullPolicy` | Dedicated local account setup image; used by guarded Kind setup/open helpers, not by Auth or hosted startup |
| `api.replicas`, `worker.replicas`, `ui.replicas`, `workflows-ui.replicas`, `management-ui.replicas` | Independent application scaling |
| `api.resources`, `worker.resources`, `ui.resources`, `workflows-ui.resources`, `management-ui.resources` | Resource requests and limits |
| `splitServices.auth/workflows/management.replicas/resources` | Per-service overrides, merged over `splitServices.resources` defaults |
| `development.enabled` | Include development Temporal, Keycloak, identity PostgreSQL and Mailpit; default true |
| `development.persistence.size/storageClass` | Storage for each development StatefulSet |
| `database.driver` | `postgres` or `mongodb` |
| `splitServices.databases.<owner>.name/existingSecret/secretKey` | Separate database name and URL Secret for each service, required in every mode |
| `splitServices.credentials.<role>` | Distinct workload token Secret; data key `token` |
| `splitServices.trustConfigMap` | Workload-token hash registry; data key `trust.json` |
| `consumer.provisioning` | Keycloak provisioner endpoint/client and existing Secret, mounted only into Auth |
| `workflowSecrets.existingSecret` | Existing Kubernetes Secret holding the persistent 32-byte vault encryption key; takes precedence over development auto-generation |
| `workflowSecrets.secretKey` | Data key within that Secret; defaults to `encryption-key` |
| `workflowSecrets.externalSecret.enabled` | Create an External Secrets Operator `ExternalSecret` instead of a generated or existing Secret reference |
| `workflowSecrets.externalSecret.secretStoreRef` | Existing `SecretStore` or `ClusterSecretStore`; provider authentication stays in the operator |
| `workflowSecrets.externalSecret.remoteRef` | Provider key/property and explicit `Base64` or `None` decoding |
| `temporal.address/namespace/taskQueue` | External frontend address, namespace and shared task queue |
| `llm.existingSecret/secretKey` | Optional provider API key file, mounted into worker only |
| `llm.baseURL/defaultModel` | LLM provider configuration |
| `auth.publicURL` | Exact browser origin for development realm import; default http://localhost:4200 |
| `auth.url/issuer/jwksURL` | Required external Keycloak endpoints in hosted mode |
| `auth.realm/clientID/googleEnabled` | Identity realm/client and configured Google login visibility |
| `ingress.enabled/className/host/tls` | Optional single-origin ingress through the UI |
| `service.uiType/uiAnnotations` | Edge Service exposure: ClusterIP by default, optionally NodePort or LoadBalancer |
| `service.annotations` | Annotations on all application Services; owner APIs remain ClusterIP |
| `autoscaling.defaults` | Shared min/max replicas, CPU/memory targets and scaling behavior |
| `autoscaling.workloads.<component>` | Enable or override HPA independently for each Deployment |
| `podDisruptionBudgets.workloads.<component>` | Optional disruption budget with an integer or percentage `maxUnavailable` |
| `serviceAccount.create/name/annotations` | Per-workload tokenless accounts, or a named existing account |
| `topologySpreadConstraints` | Spread each component across hosts/zones with its own selector |
| `nodeSelector/tolerations/affinity` | Application scheduling |

For hosted installations, customize `values-production.yaml` with pushed images and provision the three databases, workload credentials, trust ConfigMap and Temporal namespace before installing. Set `temporal.tls.enabled=true` for encrypted Temporal transport; optional `existingSecret`, `caKey`, `certKey` and `keyKey` mount CA/client-certificate files only into Workflows and worker. API-key authentication is not supported. Hosted connectivity must be verified against your endpoint.

Execution payloads are encrypted by default using a domain-separated key derived from the persistent workflow-vault key. `executionSecurity.existingKeyringSecret` enables independent execution-key rotation. `executionSecurity.allowLegacyPayloads` defaults to false; existing plaintext installations require an explicit compatibility rollout. `executionSecurity.sensitiveRunRoles` defaults to `[admin]`. Optional `auth.requiredACR` and `auth.maxAuthAge` enforce signed Keycloak authentication strength/freshness after the identity flow is configured. Read the [security rollout and limitations](../../docs/deployment.md#execution-security-configuration) before upgrading an existing installation.

The bundled identity database uses public development credentials, Temporal and Keycloak use development servers, and Mailpit captures email locally. Do not use that mode for production. Application APIs enforce verified-token login and team permissions. Hosted mode requires separately managed Keycloak with HTTPS and production SMTP; Google requires its own OAuth client. See [authentication](../../docs/authentication.md) for setup, roles, and remaining production controls.

## Autoscaling, availability and ingress

Chart 0.3.0 includes `autoscaling/v2` HPAs, `policy/v1` disruption budgets, per-workload ServiceAccounts, explicit rolling-update settings and optional topology spreading. Existing application ConfigMaps, Secret mounts, health probes, bootstrap Job, dependency PVCs and ingress remain part of the chart. No Kubernetes API permissions are granted to application accounts, and token automounting is disabled. Database bootstrap hooks also have token automounting disabled and do not depend on ServiceAccounts created later in the release.

The eight component keys are `api` (gateway), `auth`, `workflows`, `management`, `worker`, `ui` (Studio host), `workflows-ui`, and `management-ui`. Each has a Service; the worker Service exposes only its internal health port 8081, not workflow execution. Application APIs and remote UI Services remain ClusterIP. The edge UI Service exposes port 80 and proxies `/api/` to the gateway and `/remotes/` to the independent Angular applications. The separate product website is not deployed by this chart.

Local defaults leave HPA, disruption budgets and ingress disabled and use one replica. The production example enables HPA and a disruption budget for all eight Deployments: a minimum of two replicas, a CPU target of 70%, a maximum of six replicas (eight for workers, four for each UI), and at most one voluntary disruption per component. Scale-down recommendations stabilize over five minutes and reduce replicas by at most 25% per minute. These are starting values, not a capacity guarantee; tune requests, limits, database connection capacity and replica ceilings for your workload.

For example, to scale only the Workflows API with separate CPU and memory targets:

```yaml
autoscaling:
  workloads:
    workflows:
      enabled: true
      minReplicas: 2
      maxReplicas: 8
      targetCPUUtilizationPercentage: 70
      targetMemoryUtilizationPercentage: 80
podDisruptionBudgets:
  workloads:
    workflows: {enabled: true, maxUnavailable: 1}
```

Set a metric target to zero to disable that metric; at least one must be enabled. Targets use utilization relative to resource **requests**, not limits. The chart rejects missing/zero requests, invalid replica ranges and unknown component names. HPA-enabled Deployments omit `spec.replicas` so chart upgrades do not fight the autoscaler. To return to manual scaling, disable that component's HPA and set its normal `replicas` value (`splitServices.<owner>.replicas` for the three services). CPU/memory scaling is not Temporal queue-depth scaling; I/O-bound worker saturation may not appear as CPU pressure. Queue-based scaling and scale-to-zero are not included.

The cluster must supply the resource metrics API, normally through Metrics Server. HPA resources alone do not install a metrics collector. An ingress controller must also be installed and match `ingress.className`; creating an Ingress alone does not serve traffic. See [Kubernetes HPA requirements](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/) and [Ingress prerequisites](https://kubernetes.io/docs/concepts/services-networking/ingress/).

```yaml
auth:
  publicURL: https://studio.example.com
ingress:
  enabled: true
  className: YOUR_INGRESS_CLASS
  host: studio.example.com
  annotations: {}
  tls:
    - secretName: studio-tls
      hosts: [studio.example.com]
```

Point DNS at the controller and provision the TLS Secret (or use your platform's certificate controller). Configure the same browser origin in Keycloak redirect/logout settings. Hosted login uses the external `auth.url`; bundled development identity is not suitable for production. For a LoadBalancer Service without ingress, set `service.uiType=LoadBalancer` and supply provider-specific annotations through `service.uiAnnotations`; TLS termination is then a platform responsibility. Never expose Auth, Workflows or Management directly to bypass the gateway.

Disruption budgets limit voluntary evictions; they do not prevent node failures or govern Deployment rolling updates. `maxUnavailable: 1` permits downtime with a single replica, whereas `maxUnavailable: 0` can block node drains. The production example pairs budgets with at least two replicas. Deployments use `maxSurge: 1` and `maxUnavailable: 0` for rolling updates, so nodes need headroom for the extra pod. Optional topology spreading uses component-specific selectors; `ScheduleAnyway` prefers separation without making a small cluster unschedulable. See [Kubernetes disruption budgets](https://kubernetes.io/docs/tasks/run-application/configure-pdb/).

Metrics Server, ingress/certificate controllers, database high availability, cloud load balancers and cluster-wide network policies remain platform-managed. A blanket egress-deny policy is intentionally not generated because workflows call user-selected external APIs, email and model providers. Customize network restrictions for those destinations and your external databases/identity/Temporal services before production exposure.

Verify a target installation with explicit cluster context:

```bash
kubectl --context YOUR_CONTEXT -n YOUR_NAMESPACE get hpa,pdb,ingress,svc,serviceaccount
kubectl --context YOUR_CONTEXT -n YOUR_NAMESPACE describe hpa YOUR_RELEASE-workflows
kubectl --context YOUR_CONTEXT -n YOUR_NAMESPACE top pods
```

Verification on 2026-10-12 covered Helm rendering/linting and local Kubernetes API server dry-run validation of the production HPAs, budgets, accounts, Services, ingress and Deployments. The local cluster had no metrics API or ingress class installed, so live autoscaling, external DNS/TLS and ingress traffic were not load-tested or enabled there.

## Workflow vault encryption Secret

Workflows and its worker use the same Kubernetes Secret, mounted as a read-only file at `/var/run/runloom-workflow-secrets/encryption-key`. `RUNLOOM_SECRETS_KEY_FILE` contains only that path, not the key value. Auth, Management, gateway and UI applications do not mount the Secret. This encryption key protects the named values saved in Studio; those values remain encrypted in the Workflows database, not individual Kubernetes Secrets.

With development defaults, the chart creates `runloom-workflow-key` for a release named `runloom`. The name follows the chart fullname for other release names or `fullnameOverride`. The chart generates a random 32-byte binary key on first installation, reuses the installed key on upgrades, and retains the Secret on Helm uninstall. A connected upgrade refuses to invent a replacement if the Secret is missing while the Workflows Deployment still exists. Back it up separately from the database. Deleting the namespace or cluster removes it; losing or replacing the key makes existing vault values unreadable.

When a vault is configured, Workflows and worker readiness probes use `/readyz/vault`, which requires the encryption key to have loaded successfully as well as healthy database/Temporal connections. A missing or malformed mounted key fails process startup. This prevents a vault-disabled server from being considered ready in a vault-enabled installation. Local Compose uses the same vault-aware health checks. Hosted installations without a configured vault retain ordinary readiness and cannot save secrets.

To use a separately managed Secret in either development or hosted mode, place it in the release namespace and set:

```yaml
workflowSecrets:
  existingSecret: runloom-vault-key
  secretKey: encryption-key
```

The referenced data entry must decode to exactly 32 bytes. Do not put the key itself in Helm values or source control. Provision it through your secret manager, or create it from a private binary key file. For example, for the existing local cluster and namespace only:

```sh
kubectl --context kind-runloom-local --namespace runloom create secret generic runloom-vault-key \
  --from-file=encryption-key=/absolute/private/workflow-encryption-key
```

This command creates a new Secret and fails if the name already exists; it does not overwrite or rotate a key. See [key-file creation](../../docs/api-nodes-and-secrets.md#operator-setup). If switching an existing vault to a separately managed Secret, preserve the exact existing key rather than generating another one. Hosted deployments never auto-generate a key; without an existing Secret reference, vault saving is disabled.

Use a connected Helm install/upgrade for the auto-generated development key. Offline `helm template` cannot look up the installed key and generates a new one; do not apply that rendered Secret over an existing vault. Treat rendered manifests and Helm release storage as sensitive. Secret references are configuration, not a key-rotation mechanism.

### External Secrets Operator

Install External Secrets Operator with the `external-secrets.io/v1` API and configure a provider-authenticated `SecretStore` or `ClusterSecretStore` separately. The Runloom chart does not install the operator, create cloud credentials, or fetch provider secrets from application code. Supply provider references only:

```yaml
workflowSecrets:
  existingSecret: "" # Mutually exclusive with externalSecret.enabled.
  secretKey: encryption-key
  externalSecret:
    enabled: true
    targetName: runloom-external-vault-key
    secretStoreRef:
      name: platform-secrets
      kind: ClusterSecretStore
    remoteRef:
      key: runloom/vault-encryption
      property: encryption_key # Omit when the provider stores a single value.
      decodingStrategy: Base64
```

Store the base64 representation of the persistent 32-byte binary key in the provider. ESO decodes it once; the mounted file must be exactly 32 bytes. Use `None` only when the provider already returns those binary bytes. Automatic encoding detection is deliberately unsupported. Workflows and worker fail startup for an invalid key length.

The chart emits `refreshPolicy: CreatedOnce`, `target.immutable: true`, `creationPolicy: Orphan`, and `deletionPolicy: Retain`. This avoids periodic key changes, prevents rewriting an existing target, and retains the target Secret when the ExternalSecret is removed. CreatedOnce alone is not sufficient protection if an ExternalSecret is recreated; see [ESO refresh and lifecycle behavior](https://external-secrets.io/latest/api/externalsecret/#createdonce) and [explicit decoding](https://external-secrets.io/latest/guides/decoding-strategy/). Keep the provider value backed up and unchanged: deleting the target Secret can still cause recreation from the provider.

Wait for the ExternalSecret to report `Ready` before expecting Workflows/worker readiness. Missing CRDs, store permissions or provider data do not fall back to a random key. On an existing installation, first copy the exact current key to the provider through your secure process, reconcile to a separate target name, and verify equality without logging values before switching the application. Do not create a new key for an existing database. Key rotation/re-encryption is not implemented.

The chart retains StatefulSet PVCs and the generated development vault Secret after uninstall. External services and separately managed Secrets are never deleted by the chart. Reusing a PVC with a different database engine or switching databases does not migrate data.

Set `observability.enabled: true` to add backend logging/metrics sidecars and an internal Prometheus instance. No visualizer is installed. See [observability](../../docs/observability.md) for privacy, retention, resource limits, operator access and validation commands.

See the repository's [deployment guide](../../docs/deployment.md) for Compose, registry publishing, smoke tests, secrets, upgrades, and troubleshooting. The chart is distributed under the included MIT license.
# External database connections

Database nodes reuse encrypted team connections created in Management. The API
and worker use the existing `workflowSecrets` key. Private destinations require
exact operator-approved `databaseConnections.allowedAddresses` entries such as
`reporting.internal:5432`; the default is an empty list. This setting is mounted
only into Workflows and worker configuration. See [Database connections](../../docs/database-connections.md)
for TLS, least-privilege database accounts, supported operations and retry limits.
