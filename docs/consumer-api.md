# Runloom consumer API reference

This guide is for backend applications calling published workflows. It covers workflow discovery, run start/list/status/cancellation, authentication, access checks, retry safety and service-account operation. For Angular UI and human-authorized management requests, use the separate [Studio API reference](studio-api.md). See the [API index](api.md) for the interface comparison.

Interactive Swagger UI: `/api/docs/consumer/` on your API/Studio origin. Download the contract from `/api/v1/openapi.json`. The separate Studio Swagger page is `/api/docs/studio/`. Enter only a consumer access token in this page's Authorize control; never a client secret. Try it out performs real operations and keeps authorization in page memory only.

Verified against repository routes and authorization code on 2026-10-02. Examples describe implemented v1 behavior, not confirmation of a live deployment. Consumer pause/stop/resume endpoints are **not implemented**; see [stop and resume availability](#stop-and-resume-availability).

Runloom has two interfaces in the same Go backend. Studio and organization/team management use verified human login under `/api/v1/studio`. Consumer execution uses `/api/v1/workflows` and `/api/v1/runs` with a separate `runloom-execution` token audience. Consumers cannot edit/publish definitions, manage accounts, inspect another consumer's runs, invoke Studio tests, or independently start child workflows. Temporal and the existing workflow interpreter remain shared. Only `v1` is implemented; see the [API version policy](workflow-api.md#api-versions).

Keycloak authenticates applications and issues OAuth client-credentials access tokens. Runloom owns team membership, explicit workflow-version grants, scopes, quotas, run ownership and credential revocation. Tokens alone never grant team-wide access. Browser applications must use interactive sign-in; never embed a client secret in Angular, JavaScript bundles, localStorage or sessionStorage. Browser-to-machine integration requires a trusted backend holding the credential. Human APIs are available under `/api/v1/studio`; Studio tokens are not accepted by the consumer execution endpoints.

## Access checks on every request

Access requires all applicable checks, not just a valid token. Runloom does not accept a caller-supplied user ID or team role as a substitute for the authenticated service account.

| Operation | Token and account scope | Additional authorization |
| --- | --- | --- |
| List workflows or get available versions | `workflows:read` | Only the account's explicit published main workflow/version grants are returned |
| Start a run | `runs:start` | Exact definition/version grant; schema-valid input; request and concurrency quotas |
| List runs | `runs:read` | Only the account's own root runs whose definition/version is still granted |
| Get run status | `runs:read` | Same account owns the root run and retains its definition/version grant |
| Cancel a run | `runs:cancel` | Same account owns the root run and retains its definition/version grant |
| Resume, pause, stop or terminate a run | None available | Unsupported; adding a scope to a token does not create an operation |

1. The access token must pass issuer, signature, expiry, execution-audience and service-account identity checks. Its subject and credential generation must match an active registered account.
2. The required operation scope must exist in both the token and the account's current policy. Scopes are account-wide; version grants apply to the workflows on which those operations can be used. Per-workflow operation scopes are not implemented. Use separate accounts if different workflow sets need different operation permissions.
3. The account fixes the team. A conflicting `X-Runloom-Team` returns 403. Project folders are authoring organization, not consumer authorization boundaries.
4. Grants name exact published versions. Publishing a new version does not grant access to it. Drafts and child workflows are not directly callable; an allowed parent can execute its pinned child dependencies through the engine.
5. Another account's run is inaccessible even if both accounts share a team and identical workflow grants. Studio tests and child runs are not listed. Inaccessible run IDs return 404 before any Temporal lookup/cancellation.
6. Removing a version grant hides its earlier runs and denies status/cancellation. Revoking an account or rotating credentials invalidates subsequent requests under the old identity/generation; it does not stop accepted runs. Rotation with the same client ID preserves run ownership. A new account with the same display name does not inherit old runs.

For example, an account granted `invoice` v1 cannot start v2. A second account granted `invoice` v1 still cannot read or cancel the first account's run. A human Studio editor in that team can operate team runs through the separate Studio interface; this does not extend the consumer account's permissions.

## Authentication and request conventions

Base URL: `https://<your-api-origin>/api/v1`. Use the configured installation origin; local development can use HTTP. Obtain an access token from `{issuer}/protocol/openid-connect/token` using OAuth `client_credentials`, the client ID and the one-time-issued secret stored in your backend secret manager. Send only the access token to Runloom:

```http
Authorization: Bearer <consumer-access-token>
Content-Type: application/json
```

`Content-Type` is needed for JSON bodies. Do not send a Studio token, ID token, client secret or password to an execution endpoint. Do not place secrets or tokens in URLs or browser storage. Examples use placeholders and synthetic data; starts can execute real external side effects and incur costs.

Typed request bodies reject unknown fields and multiple JSON values; the request body limit is 1 MiB. Start input plus pinned definition/dependency snapshots is also bounded by the workflow contract. URL-encode path parameters. Responses are JSON with `Cache-Control: no-store`. Poll status with backoff and a caller deadline; successful HTTP acceptance is not workflow completion.

## List and inspect accessible workflows

`GET /api/v1/workflows?limit=50` returns one item per granted published main version. `GET /api/v1/workflows/invoice?limit=50` returns the same paginated envelope filtered to that definition, not a single workflow object. Both require `workflows:read`.

```http
GET /api/v1/workflows?limit=50 HTTP/1.1
Authorization: Bearer <consumer-access-token>
```

Illustrative HTTP 200 response:

```json
{
  "items": [{
    "id": "invoice",
    "name": "Process invoice",
    "version": 1,
    "input_schema": {"type":"object","required":["invoice_id"],"properties":{"invoice_id":{"type":"string"}}},
    "exposed_input_fields": [],
    "exposed_output_fields": ["receipt_id"]
  }],
  "next_cursor": ""
}
```

No workflow steps, code or credentials are returned. Schema `default` and `examples` fields are removed recursively. The catalog sorts by definition ID, then version. Unknown/ungranted definition details return 404; a catalog with no visible items returns an empty array. Use `after=<next_cursor>` for subsequent pages and stop on an empty cursor. `limit` is 1–100, default 50; both list and definition-detail routes accept it. Restart catalog pagination after a grant change.

## Start a workflow run

`POST /api/v1/runs` requires `runs:start`, an exact version grant and `Idempotency-Key` containing 8–200 characters without leading/trailing whitespace.

| Body field | Contract |
| --- | --- |
| `definition_id` | Required granted main workflow ID |
| `version` | Required explicit published version, at least 1; no implicit latest version |
| `input` | JSON object satisfying the published input schema; omitted/null becomes `{}` |

```http
POST /api/v1/runs HTTP/1.1
Authorization: Bearer <consumer-access-token>
Content-Type: application/json
Idempotency-Key: invoice-request-0001

{"definition_id":"invoice","version":1,"input":{"invoice_id":"INV-42"}}
```

Illustrative HTTP 202 response, with `Location: /api/v1/runs/<run-id>`:

```json
{"id":"<run-id>","definition_id":"invoice","version":1,"status":"running"}
```

Retain the returned `id` exactly; it identifies the root run for status/cancellation, not the workflow definition or Temporal execution ID. A worker must execute the accepted run. Repeating the same key and body under the same account resolves to the same run, including after completion; the returned status need not still be `running`. Reusing a key with a different body returns 409 `idempotency_conflict`. After a timeout or 503, retry with the **same** key and body. Never generate a new key merely because the response was lost.

## List runs and get status

`GET /api/v1/runs?definition_id=invoice&limit=50` requires `runs:read` and returns this account's retained, granted root runs. The definition filter is optional. Results use creation sequence order and the `after` cursor, not a page number. Lists are persisted summaries and can lag execution; poll individual status for fresher data.

```json
{
  "items": [{"id":"<run-id>","definition_id":"invoice","version":1,"status":"running","created_at":"2026-10-02T10:00:00Z"}],
  "next_cursor": ""
}
```

`GET /api/v1/runs/<run-id>` requires `runs:read`, ownership and a retained version grant. An illustrative HTTP 200 completed response, when `receipt_id` is allowed for disclosure, is:

```json
{
  "id":"<run-id>",
  "definition_id":"invoice",
  "version":1,
  "status":"completed",
  "created_at":"2026-10-02T10:00:00Z",
  "output":{"receipt_id":"R-42"}
}
```

`input` and `output` are omitted unless explicitly allowed by the current grant; allowed fields can still be absent or redacted. Exposure fields control what consumers **read**, not which schema-valid fields they can submit as input. Raw interpreter state, child IDs, node data and internal errors are never part of this response.

| Status | Meaning |
| --- | --- |
| `pending` | Run admission exists; Temporal acceptance has not been confirmed |
| `running` | Open execution; can include timers, approvals, callbacks, signals or a normal Pause node |
| `paused` | Individual status query detected a top-level LLM budget pause; includes a safe `pause_reason` |
| `completed`, `failed`, `canceled`, `terminated`, `timedout` | Terminal outcomes; only `completed` means success |
| `continuedasnew` | The recorded Temporal execution continued as new; not a normal result of this JSON interpreter and not a success indicator |

Treat unknown statuses defensively. Consumer status does **not** expose every ordinary Pause node as `paused`; it reports Temporal lifecycle with the budget-pause exception. A status query failure can also leave a budget-paused execution reported as `running`. Run lists do not include this live pause overlay. Budget reason values are `llm_budget_limit` and `llm_budget_accounting_unavailable`. No policy names, project spend or other accounts' usage is exposed. After Temporal retention, stored outcomes can be stale; a missing outcome is not assumed successful.

## Cancel a run

`POST /api/v1/runs/<run-id>/cancel` requires `runs:cancel`, ownership and a retained definition/version grant. No body is needed.

```http
POST /api/v1/runs/<run-id>/cancel HTTP/1.1
Authorization: Bearer <consumer-access-token>
```

HTTP 202 response:

```json
{"id":"<run-id>","cancel_requested":true}
```

Cancellation is cooperative and asynchronous. Poll using an account also granted `runs:read`; a cancel-only account cannot verify the result itself. The run may complete before cancellation takes effect. A confirmed terminal run can still return this acknowledgment without another Temporal cancellation. A pending run without a confirmed execution ID returns 409 `start_pending`; reconcile via status before retrying. Repeated cancel requests may create repeated audit events; start-request idempotency does not apply to this route. Cancellation cannot undo external side effects or resume a closed run.

## Stop and resume availability

There are no consumer `/stop`, `/pause`, `/resume` or `/terminate` routes in v1, and no supported corresponding scopes. With valid configured consumer authentication, these POST actions return 404 `not_found`; authentication/configuration checks can fail first. They are deliberately absent from OpenAPI, not undocumented aliases.

If “stop” means permanently stop work, use cancellation above. If it means temporarily suspend and later continue, the consumer API cannot do that today. A workflow can reach its designed Pause node or be paused by a budget policy. An authorized human operator can use [Studio resume](studio-api.md#stop-and-resume-semantics), addressing the actual paused run/step and checking the result. Do not hand the consumer human credentials to bypass the boundary. Resume is also not approval, callback delivery or named-signal delivery; those actions remain Studio-only.

Adding consumer pause/resume is a separate implementation task requiring operation scopes, ownership and current-grant enforcement, safe pause/checkpoint targeting, invalid-state behavior, idempotency, auditing and updated OpenAPI/tests. No future route or scope is promised by this document.

## Errors and retries

Consumer errors have a separate contract from Studio:

```json
{"error":{"code":"workflow_forbidden","message":"Workflow version is not granted."}}
```

| HTTP status | Codes | Caller action |
| --- | --- | --- |
| 401 | `unauthorized`, `revoked` | Obtain valid service-account credentials/token; revoked or old-generation tokens cannot be reused |
| 403 | `forbidden`, `insufficient_scope`, `workflow_forbidden` | Check account team, token/account scopes and exact version grants |
| 404 | `not_found` | Unknown/inaccessible resource or unsupported action; do not infer another account's run exists |
| 409 | `idempotency_conflict` | Original key was used with different data; reconcile rather than blindly issuing a new key |
| 409 | `start_pending`, `start_uncertain` | Pending: retry status; uncertain: operator review before any new start |
| 422 | `invalid_request`, `invalid_input`, `invalid_workflow`, `invalid_pagination`, `idempotency_key_required`, `child_only` | Correct the request; direct child starts are not allowed and normally fail the grant check first |
| 429 | `rate_limit`, `concurrency_limit` | Respect `Retry-After: 60`; active/waiting roots retain concurrency slots |
| 503 | `not_configured`, `retention_unsafe`, `unavailable` | Configuration/dependency failure; reconcile an uncertain start using the same key/body |

401 responses include `WWW-Authenticate: Bearer realm="runloom-execution"`. Read status with backoff; do not retry permission/validation errors unchanged. A timeout or storage failure after dispatch may mean the operation took effect. See [durability and quotas](#durability-quotas-and-operational-limits) for ambiguous-start recovery and retention constraints.

## Operator setup

Service-account provisioning is automatic in the guarded local setup commands and explicitly configured by operators for hosted deployments. Ordinary login and existing workflows do not require a provisioning credential. Do not use a realm-admin token as the API's permanent credential.

`make k8s-up`, `make k8s-install` and `make k8s-upgrade` automatically configure and verify the dedicated provisioner after Keycloak is ready and before reporting setup success. Setup uses its own temporary loopback port-forward; no browser or `k8s-run` session is required. The credential is mounted only into the API as a Kubernetes Secret. A private backup is under `.config/runloom/local/<cluster>/<namespace>/<release>-provisioner-secret` in your user home. `make k8s-consumer-setup` remains an optional verification/repair command for older installations without rebuilding images, not a required setup step. Custom operator settings are rejected rather than overwritten.

`make local-up` and `make fresh-up` also create/verify the provisioner automatically before starting the API and include `compose/compose.consumer.yaml`. Their private credential files are separate per project, under `.config/runloom/local/compose/<project>-provisioner-secret` in your user home. An explicit `RUNLOOM_PROVISIONER_SECRET_PATH` or existing API mount is preserved. Managed files are `0640`, and only the API receives the corresponding supplementary group; explicit file permissions are not changed. Direct Compose operators can set `RUNLOOM_PROVISIONER_SECRET_GID` for a group-readable credential.

Local setup uses the public development administrator for first creation and removes temporary administrator-token files. Subsequent setup authenticates with the existing provisioner credential. After a fresh local cluster/identity installation, a rejected host backup is checked against Keycloak: if the client is absent, setup recreates it with that same saved credential, leaving the file unchanged. An existing client, an invalid Kubernetes Secret, or an explicitly supplied credential is never overwritten or rotated. Errors distinguish administrator login from provisioner verification. Restore the matching original credential when an existing client rejects it. These commands do not create end-consumer accounts or start workflows. Create consumer credentials from **Management → Service accounts** after setup. A `service-account provisioning is not configured` error means the API lacks provider configuration; the workflow-vault encryption key is unrelated.

Hosted deployments and custom local credentials use the operator procedure below. The local shortcut does not apply to staging or production.

1. Start Keycloak using the existing deployment. Obtain a short-lived realm-admin access token using your organization's administrative login process, and save it in a private `0600` file outside the repository. The bootstrap needs permission to create client scopes/clients and assign the realm-management client roles. Never put tokens in URLs, command-line values, screenshots or logs.
2. Run the operator bootstrap once. It creates the four execution client scopes if absent, then creates `runloom-provisioner` with client-management roles (`manage-clients`, `view-clients`, `query-clients`). It generates a random provisioner secret, saves it to a new `0600` file, and never prints the value. Existing output files are not overwritten. On partial failure, inspect the named client and output file before retrying; the script deliberately does not delete or rotate existing identities.

```sh
node scripts/setup-consumer-auth.mjs \
  http://localhost:18081/auth runloom \
  /absolute/private/admin-access-token \
  /absolute/private/new-provisioner-secret
```

3. Treat the provisioner as a privileged infrastructure credential. Its built-in client-management roles can manage other clients in this realm, not just Runloom consumers. Use a dedicated Runloom realm, restrict administrative network access, and use Keycloak fine-grained administration if narrower provider-side permissions are required. Do not expose its credentials to Studio users. Remove the temporary admin token file through your normal secure cleanup process after setup.
4. Configure the API with the values below and restart/rebuild the API and Studio. The worker requires no new auth credentials or workflow type. Use HTTPS for hosted identity and consumer endpoints.

| Variable | Meaning |
| --- | --- |
| `RUNLOOM_EXECUTION_AUDIENCE` | Dedicated resource audience; defaults to `runloom-execution`, must differ from Studio client ID |
| `RUNLOOM_KEYCLOAK_ADMIN_URL` | API-reachable Keycloak base URL, including `/auth` when used |
| `RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID` | `runloom-provisioner` |
| `RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE` | Read-only file containing the provisioner secret; there is no secret-valued environment variable |

Keycloak must retain its `basic` scope for service-account subjects. The bootstrap adds `workflows:read`, `runs:start`, `runs:read` and `runs:cancel` scopes without replacing existing realm or Studio configuration. New consumer clients have browser, implicit and password grants disabled and refresh tokens disabled. Only selected execution scopes are assigned. Provisioning uses the Keycloak Admin REST API; Runloom never retrieves an existing consumer secret from that API.

## Docker Compose and Helm

For Compose, first bootstrap against the running Keycloak, then enable the overlay:

```sh
RUNLOOM_PROVISIONER_SECRET_PATH=/absolute/private/new-provisioner-secret \
  docker compose --project-directory . -f compose/compose.yaml -f compose/compose.consumer.yaml up --build --detach --wait
```

The secret is mounted only into the API at `/run/secrets/runloom-provisioner`. File-backed Compose secrets can retain host ownership/mode; ensure the file is readable by container UID 65532 without making it world-readable. Native development uses the same private file under the API process's user. Never commit the file or add the secret value to `.env`.

For Helm, create an existing Kubernetes Secret through your secret-management process, then set:

```yaml
consumer:
  audience: runloom-execution
  provisioning:
    enabled: true
    adminURL: https://identity.example.com/auth
    clientID: runloom-provisioner
    existingSecret: runloom-provisioner
    secretKey: client-secret
```

The chart mounts the Secret read-only into the API only. It does not provision a realm, install admin credentials or change a live Keycloak configuration. Bootstrap/configure the target realm separately before enabling this setting. Existing realm imports are not rewritten.

## Make shortcuts

Start Keycloak and obtain the private admin-token file as described above before running bootstrap. These commands run from the repository root. Paths may contain spaces; keep them quoted. `KEYCLOAK_URL` and `KEYCLOAK_REALM` default to `http://localhost:18081/auth` and `runloom` and can be overridden for operator setup.

```sh
export RUNLOOM_ADMIN_TOKEN_FILE="/absolute/private/admin-access-token"
export RUNLOOM_PROVISIONER_SECRET_PATH="/absolute/private/new-provisioner-secret"
make setup-consumer-auth
make docker-config-consumer
make docker-up-consumer
```

Bootstrap refuses an existing output file; the Compose targets require a readable existing secret file. The secret value is never passed as a Make variable or printed. `docker-config-consumer` only validates configuration. `docker-up-consumer` builds and starts the complete stack with the provisioning secret mounted; use `docker-up-consumer-mongo` instead for MongoDB. Use `make docker-logs` and `make docker-down` for either variant; shutdown retains database volumes. Running ordinary `make docker-up` later recreates the API without the consumer provisioning overlay, so keep using the consumer target when provisioning should remain enabled.

`make test-consumer` runs the consumer and API package tests. `make test-ui-browser` runs browser regression scripts and requires the development UI and Playwright; see [contribution checks](../CONTRIBUTING.md#checks-before-a-pull-request). `make test-keycloak-live` creates and removes an isolated test Keycloak container. `RUNLOOM_TEST_TEMPORAL_ADDRESS=127.0.0.1:7233 make test-temporal-live` exercises consumer execution and schedule/child workflow integration against a disposable Temporal server; a missing address fails instead of silently skipping tests. These explicit live targets are not prerequisites of `make verify`.

## Team administrator workflow

1. Sign into Studio and open **Workspace settings → Service accounts**. Team admins and organization owners/admins can manage these accounts; editors/viewers cannot.
2. Enter a name, select execution scopes, request rate and maximum concurrent root runs. Add one or more published main workflows and explicit allowed versions. A new published version is not automatically exposed.
3. Optionally allow named top-level input/output fields to be returned. Empty lists expose neither. No wildcard is supported. Nested keys containing password, secret, token, authorization, API key or cookie names are redacted even when their parent is allowed. This is not a general personal-data classifier: review every exposed business field. Original data still exists in Temporal/internal run storage for execution and authorized Studio audits.
4. Create credentials. Copy the client ID and secret into the consuming backend's secret manager, then select **I saved the secret — hide it**. The secret is present only in the creation/rotation response and component memory. It is not stored in Runloom's database, browser persistence or audits. It cannot be retrieved through a list/detail endpoint. Lost response or lost secret requires rotation; a lost creation response can be resolved by listing the created account and rotating it.
5. Use **Edit permissions** for workflow grants, disclosure fields and quotas. Scopes are read-only after creation; create a new account to use different scopes. The API rejects both scope additions and removals with 422. Use **Rotate secret** to keep the client ID and run ownership but replace the secret and token generation. Old access tokens are rejected by Runloom on subsequent requests, not merely after token expiry. An incomplete rotation fails closed and can be retried; a stale in-progress operation can be retried after one minute.
6. **Revoke account** immediately blocks subsequent Runloom requests even if Keycloak is unavailable. It also attempts to disable the Keycloak client. If provider cleanup fails, the UI says so; retry revoke. Already accepted/in-flight requests and running workflows are not rolled back or canceled by revocation.

“One-time visible” is a Runloom application guarantee, not a claim that privileged Keycloak administrators cannot access provider-managed client credentials. Responses use `Cache-Control: no-store`. Configure proxies/APM to avoid recording Authorization headers and credential-response bodies.

## Management endpoints

Credential creation, policy updates, rotation and revocation belong to the [Studio management API](studio-api.md#credentials-and-cost-management), using a human token and effective team-admin permission. The request policy is documented in [service account policies](studio-api.md#service-account-policies). There is no consumer-accessible credentials route.

## Consumer contract

The machine-readable [OpenAPI document](../server/workflows/internal/api/consumer.openapi.json) is served at `GET /api/v1/openapi.json` without credentials. It covers the six implemented operations documented above; it does not expose Studio routes or consumer pause/resume. See [access checks](#access-checks-on-every-request), [status and disclosure](#list-runs-and-get-status), and [errors and retries](#errors-and-retries) before generating or integrating a client.

| Method | Path under `/api/v1` | Required scope | Success |
| --- | --- | --- | --- |
| GET | `/workflows` | `workflows:read` | 200, `{items,next_cursor}` |
| GET | `/workflows/{id}` | `workflows:read` | 200, `{items,next_cursor}` containing granted versions |
| POST | `/runs` | `runs:start` | 202, `{id,definition_id,version,status}` |
| GET | `/runs` | `runs:read` | 200, `{items,next_cursor}` |
| GET | `/runs/{id}` | `runs:read` | 200, owned run status and optional permitted data |
| POST | `/runs/{id}/cancel` | `runs:cancel` | 202, `{id,cancel_requested:true}` |

## Durability, quotas and operational limits

1. A compare-and-swap ledger reserves the idempotency key and root-run quota before Temporal starts. Payload snapshots are immutable, separately stored documents. PostgreSQL and MongoDB share reservations/rate counters across replicas; filesystem mode supports one API process. Do not delete admission records or restore only part of the application database.
2. Starts use a stable run ID with Temporal reject-duplicate policies. A lost acceptance response is reconciled by describing that ID. An ambiguous start may be resubmitted with the same ID only within five minutes; the API verifies Temporal namespace retention is at least one hour first. Never administratively purge executions or reduce retention during that window. Older unresolved starts fail closed with `start_uncertain`; their capacity stays reserved until an operator determines the outcome. A new idempotency key could duplicate business work.
3. This is retry-safe run admission, not exactly-once activity side effects. Activities still need their own idempotency when calling external systems. No automatic cancellation is attempted merely because storing the start response/audit failed.
4. Rate limits use a durable UTC minute bucket per account. Concurrency is per account's root runs, including waiting workflows. Terminal runs free capacity when reconciled on status/start. Unknown/unavailable status retains capacity; an outage does not release it. Child fan-out consumes worker capacity but is not counted as additional root quota. Review supported per-team task-queue fairness and worker capacity before high-volume multi-tenant use; this feature does not enable fairness automatically.
5. Database-backed admission stores each run identity separately and never silently expires idempotency keys. The former 10,000-run lifetime cutoff is removed. Rate counters use a separate revision scope, so reads do not rewrite the run ledger. Admission still reads the account aggregate, so capacity testing is required before high-scale use; filesystem storage retains its 8 MiB document bound. Account credentials/grants use their own normalized metadata scope.
6. Runloom stores workflow inputs/outputs in its team store plus immutable consumer input snapshots. Response redaction and at-rest encryption are separate controls. PostgreSQL/MongoDB execution payloads and admission snapshots use the configured execution-encryption key; see [coverage and limitations](database.md#stage-1-execution-security). Human payload access requires an authorized role. Back up application state, Temporal and key material together, storing keys separately. After Temporal retention, persisted status/output may be older; an unknown outcome is not converted to success. Audit-completion failures may return 503 after execution was accepted; retry starts with the same idempotency key.

## Verification

For the focused authorization contract, run `go test ./internal/api -run 'TestConsumer|TestGatewayTenantAndRoleBoundaries|TestAPIVersionRouting'` from `server/`. These handler tests use synthetic identities and mocked Temporal/provider calls; they do not prove live deployment behavior. `make test-ui-browser` checks Studio integration against a development server with mocked API calls. Each independent Angular project builds from its own `studio/projects/<project>` directory; there is no root Studio build package.

The documented routes and access decisions were checked against [consumer handlers](../server/workflows/internal/api/consumer.go), [policy validation](../server/workflows/internal/consumer/model.go), [gateway separation](../server/workflows/internal/api/gateway.go), [service-account management](../server/auth/internal/api/service_accounts.go), and the [consumer OpenAPI contract](../server/workflows/internal/api/consumer.openapi.json). Regression coverage includes [scope/grant/ownership and redaction tests](../server/workflows/internal/api/consumer_test.go). Documentation changes do not implement consumer pause/resume or alter authorization.

`RUNLOOM_TEST_KEYCLOAK=1 go test ./internal/consumer -run TestKeycloakProvisioningLive -count=1` creates a disposable Keycloak Docker container, provisions clients, verifies scope/audience claims and secret rotation, then removes that container. It never alters an existing realm. `RUNLOOM_TEST_TEMPORAL_ADDRESS=127.0.0.1:7233 go test ./internal/api -run TestConsumerExecutionLive -count=1` exercises real execution/retries/cancellation against a disposable Temporal server. `TestRepositoryCAS` additionally tests PostgreSQL/MongoDB when `RUNLOOM_TEST_POSTGRES_URL`/`RUNLOOM_TEST_MONGODB_URL` point to disposable databases.

References: [Keycloak service accounts](https://www.keycloak.org/docs/latest/server_admin/#_service_accounts), [Keycloak Admin REST API](https://www.keycloak.org/docs-api/latest/rest-api/index.html), [OAuth token privilege restriction](https://www.rfc-editor.org/rfc/rfc9700.html#section-2.3).
