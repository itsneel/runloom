# Studio workflow execution API

This is the detailed human-authorized run guide accompanying the [Studio API reference](studio-api.md). Backend integrations must use the separate [consumer API reference](consumer-api.md); consumer pause/resume is not implemented. The [API index](api.md) compares available operations.

## API versions

All application APIs use an explicit major version in the URL. Version `v1` is the only implemented version. Studio and management use `/api/v1/studio`; service-account execution retains its existing `/api/v1/workflows` and `/api/v1/runs` paths. These interfaces have separate token audiences and authorization rules; sharing the `v1` prefix does not make their credentials interchangeable.

Public startup endpoints are `GET /api/v1/health` and `GET /api/v1/auth/config`. Infrastructure probes `/healthz` and worker `/readyz` remain unversioned because they are operational probes, not application APIs. Keycloak `/auth` and external provider API URLs are unaffected. The consumer OpenAPI document remains at `/api/v1/openapi.json`.

This change removes the old unversioned Studio paths; they return HTTP 404 with no redirects or fallback aliases. Update bookmarks, scripts and saved callback/webhook URLs. Deploy the backend, all three Studio apps and health checks together; the platform library release `1.1.0` uses these routes. Existing running deployments are not updated merely by changing repository files.

Additive changes may stay in `v1`. Breaking request/response, authentication or semantic changes require a separately implemented `v2` contract, a migration guide and an explicit support/deprecation plan for `v1`. Never silently map an unknown version to the latest one. API versions are independent of workflow definition/publishing versions, chart versions and Temporal workflow-code versioning.

`GET /api/v1/studio/runs/{id}/llm-usage` provides paginated LLM attempt records and parent/child cost summaries. `GET /api/v1/studio/runs?include_usage=true` adds bounded run-history summaries. See [LLM accounting API and permissions](llm-costs.md#studio-api).

Studio editors can preview JavaScript with `POST /api/v1/studio/code/test`; see the [Custom Code API contract](custom-code.md#studio-preview-api). This preview is separate from workflow execution and unavailable to consumer service accounts.

This page documents the existing **human-authenticated Studio API**. Backend integrations should use the separate [consumer `/api/v1` API](consumer-api.md), which adds service accounts, explicit grants, retry-safe idempotency, quotas and consumer-owned run visibility. Do not give machine clients human editor credentials.

For recurring or one-time starts and reusable parent/child workflows, see [schedules and subworkflows](schedules-and-subworkflows.md). The existing status/cancellation endpoints also apply to scheduled and child runs.

Use this API to start a published workflow, read its live status, and request cancellation. Examples describe the current Go HTTP API, not the Temporal server API.

Only main workflows can use the normal start API. Published definitions with `kind: "child"` reject normal starts, webhook starts and schedule creation with HTTP 422; production execution requires a parent's Subworkflow node. Status, history, callbacks, signals, resumes and cancellation still work for existing child runs.

Studio's **Test workflow** and **Test child workflow** actions use `POST /api/v1/studio/definitions/{id}/test`. A verified editor/admin supplies `{"definition":<complete current workflow JSON>,"input":{"name":"Avery"}}`. The ID must match an existing workflow draft in the selected team. The server normalizes the submitted snapshot to version `0`, validates it and its input, and pins published subworkflow dependencies. It does not save or publish anything. HTTP 201 returns `workflow_id`, `run_id`, `definition_id`, `definition_version: 0` and `is_test: true`. Run history records `trigger_type: "draft_test"`; real activities and costs still apply. Invalid definitions, input, mixed version/snapshot requests or dependencies return 422; missing team-local drafts return 404. Failed starts return 502; failed history persistence returns 503 and attempts cancellation. There is no retry/idempotency guarantee. The older `{"version":1,"input":{}}` form remains available for published child tests only, recorded as `child_test`. This human-authorized endpoint is callable outside the browser with valid editor credentials; service accounts cannot use it. Production starts remain publication-restricted. See [independent child tests](schedules-and-subworkflows.md#independent-child-tests).

Workflows are organized into team-owned project folders for creating and testing workflows. [Project APIs](projects.md) cover creation, renaming, scoped listing, and workflow moves. Projects are not execution boundaries: start a published workflow with its team-wide definition ID and optional version, without a project ID or `X-Runloom-Project` header. Moving or renaming a project does not block executions or change execution/callback URLs. Status and cancellation use the run ID and authenticated team context only.

| Action | Method and path | Required team role | Success |
| --- | --- | --- | --- |
| Start | `POST /api/v1/studio/runs` | Editor or admin | `201 Created` |
| List | `GET /api/v1/studio/runs` | Viewer, editor, or admin | `200 OK`, bounded array |
| Status | `GET /api/v1/studio/runs/{workflow_id}` | Viewer, editor, or admin | `200 OK` |
| Resume | `POST /api/v1/studio/runs/{workflow_id}/resume` | Editor or admin | `200 OK`, signal accepted |
| Cancel | `POST /api/v1/studio/runs/{workflow_id}/cancel` | Editor or admin | `200 OK` |

Organization owners/admins have effective admin access to every team in their organization. All responses shown below are JSON.

## Authentication and setup

1. Start the stack with `make docker-up`. The studio proxy serves the API at `http://localhost:4200`; the direct Go API is at `http://localhost:8080`. Hosted deployments should use their HTTPS studio origin.
2. Sign in with a verified account and obtain a short-lived Keycloak **access token**, not an ID token. See [authentication setup](authentication.md). There is no application password-login endpoint. Service-account credentials are supported only on the separate consumer interface, not these human endpoints.
3. Call `GET /api/v1/studio/me` with `Authorization: Bearer <access-token>` to find your accessible organization/team IDs. Select a team where you are an editor/admin. The team header is not required for `/api/v1/studio/me`.
4. Create and publish a workflow in that team. A saved draft (version `0`) cannot be started. For the examples below, import `examples/employee-onboarding.json` in the studio and publish it. Alternatively, save it with `POST /api/v1/studio/definitions`, then call `POST /api/v1/studio/definitions/employee-onboarding/publish`, using the same auth/team headers. See [versioning](versioning.md).

Every workflow request requires both headers:

```http
Authorization: Bearer <access-token>
X-Runloom-Team: <team-uuid>
```

For the curl examples, put those two header lines, with real values, in a private file outside the repository. Set its permissions to `600`. Do not commit it, print it, or put tokens in URLs or shell history. Remove the file after testing and replace expired tokens through the normal sign-in flow.

```bash
RUNLOOM_BASE_URL=http://localhost:4200
RUNLOOM_AUTH_HEADERS_FILE=/absolute/private/runloom-headers
chmod 600 "$RUNLOOM_AUTH_HEADERS_FILE"
```

The commands below run in Bash, require curl with `--fail-with-body` support, and use jq to read the returned ID. Run them in the same shell. Credentials are loaded from the header file, not embedded in command arguments. Run IDs used in URL paths must be URL-encoded; the generated IDs in these examples contain only URL-safe characters.

## 1. Start a workflow

`POST /api/v1/studio/runs`

The request is one JSON object. Unknown fields are rejected; the maximum request body is 1 MiB.

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `definition_id` | String | Yes | ID of a published definition in the selected team |
| `version` | Integer, at least `1` | No | Exact published version; omit or use `null` for the latest published version |
| `input` | JSON object | No | Must satisfy the definition's input schema; omitted or `null` becomes `{}` |
| `workflow_id` | String | No | Optional custom ID suffix; omitted or empty generates a UUID |

Request example:

```json
{
  "definition_id": "employee-onboarding",
  "version": 1,
  "input": {
    "employee_name": "Alex",
    "department": "Engineering"
  }
}
```

Start and retain the returned ID:

```bash
START_RESPONSE="$(curl --silent --show-error --fail-with-body \
  --request POST "$RUNLOOM_BASE_URL/api/v1/studio/runs" \
  --header @"$RUNLOOM_AUTH_HEADERS_FILE" \
  --header 'Content-Type: application/json' \
  --data '{"definition_id":"employee-onboarding","version":1,"input":{"employee_name":"Alex","department":"Engineering"}}')" || exit 1
WORKFLOW_ID="$(printf '%s' "$START_RESPONSE" | jq -er '.workflow_id')" || exit 1
printf '%s\n' "$START_RESPONSE" | jq .
```

Example `201 Created` response (identifiers are illustrative):

```json
{
  "workflow_id": "11111111-1111-4111-8111-111111111111-550e8400-e29b-41d4-a716-446655440000",
  "run_id": "22222222-2222-4222-8222-222222222222",
  "definition_id": "employee-onboarding",
  "definition_version": 1
}
```

`workflow_id` is the complete team-prefixed ID. Use it exactly as returned for status and cancellation; do not add the prefix again. `run_id` identifies the underlying Temporal execution and is not accepted as the path identifier by these endpoints. There is no `run_id` selector for status or cancellation in this API.

The API returns after Temporal accepts the execution and the application persists its run record, not after the workflow completes. A worker must be running on the configured task queue to execute it. The chosen published definition and input are fixed for that execution; later definition edits do not change it.

Custom ID suffixes are prefixed with `<team-uuid>-`. The resulting ID must be at most 200 bytes, leaving at most 163 bytes for a custom suffix with a standard UUID team ID. Prefer fresh generated IDs. Custom IDs are **not** a durable idempotency-key contract: the API uses Temporal SDK defaults, which can return an existing running execution or permit reuse after a previous execution closes. Duplicate IDs can overwrite filesystem run records; PostgreSQL/MongoDB instead reject the duplicate record, causing a `503` and an attempted cancellation that may target the existing execution. Do not blindly retry an ambiguous start response or assume a retry creates exactly one new run; inspect team runs before retrying.

## 2. Get workflow status

`GET /api/v1/studio/runs/{workflow_id}`

No request body is needed.

```bash
curl --silent --show-error --fail-with-body \
  "$RUNLOOM_BASE_URL/api/v1/studio/runs/$WORKFLOW_ID" \
  --header @"$RUNLOOM_AUTH_HEADERS_FILE"
```

Example `200 OK` response when the live workflow query is unavailable:

```json
{
  "workflow_id": "11111111-1111-4111-8111-111111111111-550e8400-e29b-41d4-a716-446655440000",
  "temporal_status": "running",
  "state": null
}
```

The API describes the execution in Temporal, then attempts its `status` query. `state: null` is a valid response, for example when the worker cannot answer the query. It does not imply completion or failure. If the Temporal describe call itself fails, the endpoint returns an error instead of a stored snapshot.

### Inspect a run in Studio

1. Open **Run history** for the team and select the workflow name beside the run ID. **Run details** opens over the list. Choose **Close details** or press **Escape** to return to the same position. Access to execution details follows the team's configured sensitive run roles.
2. In **Execution timeline**, read the bars from left to right. Their position gives the start time relative to the first node and their width shows duration. Overlapping bars show concurrent execution. Select a row to see its local start and finish times, input preview, output and any node error. Open runs refresh every five seconds after each successful read.
3. For a signal handler or repeated loop body, use the invocation and parent fields to identify the execution. The timeline covers the selected run. A child workflow has its own run history.
4. The Studio history request accepts an optional `trace=true` query parameter. The worker derives the trace from the selected Temporal execution. It includes up to 1,000 node invocations, with 8 KiB per input or output preview and 384 KiB of previews in total. Larger values are marked as omitted. If a worker is unavailable or Temporal retention has expired, Studio shows stored node results and the application audit trail. Refresh runs to retry.

### Execution status versus builder state

`temporal_status` is the authoritative execution lifecycle. The current backend lowercases the pinned Temporal SDK's status names; use these exact wire values:

| `temporal_status` | Meaning |
| --- | --- |
| `running` | Execution remains open, including timers, approvals, callbacks, signals, and operator pauses |
| `completed` | Execution finished successfully |
| `failed` | Execution failed |
| `canceled` | Execution acknowledged cancellation; single `l` in this field |
| `terminated` | Execution was forcibly terminated externally; this API exposes cancellation, not termination |
| `timedout` | Execution timed out; the current wire value has no underscore |
| `continuedasnew` | The described execution continued as a new run; not a normal result of the current JSON interpreter |

Handle unrecognized statuses defensively rather than treating them as successful completion.

When `state` is available, it contains these fields:

| Field | Meaning |
| --- | --- |
| `status` | Builder state: `running`, `waiting_callback`, `waiting_signal`, `paused`, `completed`, `failed`, or `cancelled` |
| `definition_id`, `definition_version` | Definition snapshot used by the run |
| `current_step` | `{ "id": "...", "name": "...", "type": "..." }` or `null` |
| `completed_steps` | Completed step IDs |
| `steps` | Step ID to `{ "status": "...", "output": ... }` map |
| `waiting_approval`, `waiting_callback`, `waiting_signal`, `paused_step` | Current top-level gate object, or `null` |
| `waiting_approvals`, `waiting_callbacks`, `waiting_signals`, `paused_steps` | Gate maps keyed by step ID; inspect these for parallel branches |
| `error` | Error string or `null` |
| `started_at`, `completed_at` | Timestamps when available; completion timestamp is not guaranteed for every terminal outcome |
| `output` | Workflow result when available; may be omitted |

An approval can leave `state.status` as `running`; inspect the approval gate fields. Likewise, a parallel branch can wait without changing the top-level builder status. A JSON pause node keeps `temporal_status` as `running`. The builder spells its cancellation state `cancelled` (double `l`), unlike Temporal's `canceled`. Prefer `temporal_status` when deciding whether execution has ended.

Poll every few seconds with a client-side deadline and backoff on transient errors. Stop normal execution polling on `completed`, `failed`, `canceled`, `terminated`, or `timedout`. Reading live status also attempts to refresh the persisted snapshot; a successful live read does not guarantee that this best-effort persistence update succeeded.

For persisted inputs, output snapshots, and audit events, use `GET /api/v1/studio/runs/{workflow_id}/history`. For recent runs in the selected team, use `GET /api/v1/studio/runs?limit=50` (default 50, maximum 200). These are application records, not raw Temporal Event History; they can lag the live execution. The history endpoint is also useful after Temporal retention has removed an execution, as long as the application record remains.

## 3. Cancel a workflow

`POST /api/v1/studio/runs/{workflow_id}/cancel`

No body is required. Do not use `DELETE`, the definition ID, or the Temporal `run_id`.

```bash
curl --silent --show-error --fail-with-body \
  --request POST "$RUNLOOM_BASE_URL/api/v1/studio/runs/$WORKFLOW_ID/cancel" \
  --header @"$RUNLOOM_AUTH_HEADERS_FILE"
```

`200 OK` response:

```json
{
  "cancelled": true
}
```

Despite the response field's name, this acknowledges a **cancellation request**, not confirmed termination. Cancellation is asynchronous and cooperative. Poll the status endpoint until `temporal_status` is `canceled` or another terminal outcome is observed; the workflow may complete before the request takes effect. An unavailable worker or an activity that has not yet observed cancellation can delay it. Cancellation is not a pause, cannot resume a closed execution, and does not undo already completed external side effects.

The API records `workflow.cancel_requested` with the authenticated actor. It does not synchronously change the persisted run status. Do not depend on a repeated cancellation request returning a particular success/error result for an already closed execution; read its status instead.

## 4. Resume a paused workflow

`POST /api/v1/studio/runs/{workflow_id}/resume`

Use the same human access-token/team headers as other Studio run operations. Editors/admins may resume team runs, including consumer-created roots and child runs. Service-account credentials are rejected here. First read current status and select the exact `step_id` from `state.paused_step` or the `state.paused_steps` map. For a child pause, use the child's workflow ID, not its parent's ID.

```bash
curl --silent --show-error --fail-with-body \
  --request POST "$RUNLOOM_BASE_URL/api/v1/studio/runs/$WORKFLOW_ID/resume" \
  --header @"$RUNLOOM_AUTH_HEADERS_FILE" \
  --header 'Content-Type: application/json' \
  --data '{"step_id":"review-pause","payload":{"note":"Ready to continue"}}'
```

`step_id` must be nonempty and at most 64 characters. `payload` is optional JSON data passed to the paused step; use `{}` when nothing is needed. The trusted actor is taken from authentication. The request body is limited to 1 MiB and unknown fields are rejected. The HTTP 200 response is:

```json
{"accepted":true}
```

This acknowledges a signal, not confirmed continuation. The handler does not check the current pause before accepting the signal; an early event may be buffered by step ID. Do not use this as a preauthorization mechanism or blindly resend after an ambiguous response. Read status afterward to verify the gate cleared. The route has no durable idempotency-key contract. A hard-budget pause rechecks its policy/accounting and can stay paused. Resuming never bypasses budget enforcement.

Resume does not satisfy approvals, callbacks or named-signal waits. Those use their own endpoints in the [Studio event catalog](studio-api.md#runs-and-workflow-events). A completed, failed or canceled execution cannot be resumed. Temporal signalling failures use the existing mapped 404/502 errors; acceptance is not proof that a named step exists or consumed the event. Resume requests are audited as `workflow.resume_requested`.

## Stop and pause distinctions

There is no Runloom run `/stop`, `/pause` or `/terminate` endpoint. For permanent stopping, request cancellation and observe the final status. For planned suspension, define a Pause node or rely on the applicable hard-budget policy, then use resume when appropriate. Cancel is not a reversible pause. The schedule `/pause` and `/resume` actions only govern future scheduled triggers and do not suspend or resume an existing run.

## Errors and retry safety

Application errors use this shape:

```json
{
  "detail": "viewer access is read-only"
}
```

| HTTP status | When it can occur |
| --- | --- |
| `401` | Missing, invalid, expired, wrong-client, or unverified-email access token |
| `403` | Missing/unauthorized team, or a viewer attempting start/cancel |
| `404` | Definition/version not found at start, run not recorded in the selected team, or execution no longer found in Temporal |
| `422` | Malformed start JSON, unknown fields, invalid input/schema, version below 1, or an oversized custom workflow ID |
| `502` | Temporal rejects a start or fails a status/cancel operation (other than mapped not-found errors) |
| `503` | Account directory/team storage unavailable, or persistence of a newly accepted run fails |

The gateway deliberately returns `404` for a run outside the selected team's records, without querying or signalling that execution. A wrong team membership itself returns `403`.

If recording a newly started run fails, the API returns `503` and attempts cancellation, but does not guarantee that cancellation succeeded. A timeout or lost HTTP response can also leave an accepted execution running. Treat start/cancel transport errors as potentially ambiguous. Status reads may be retried with backoff; starting again can cause duplicate business work. These endpoints do not provide transactional exactly-once start semantics.

See [authentication](authentication.md) for identity setup and permissions, [workflow versioning](versioning.md) for publishing, and [the JSON contract](json-contract.md) for node/gate behavior.
