# Runloom Studio API reference

This guide covers the APIs used by the Angular host, workflow builder and management application: login context, projects, definitions, versions, draft tests, run controls, teams, credentials and analytics. Base path: `/api/v1/studio`. Backend integrations must use the separate [consumer API reference](consumer-api.md); human and service-account credentials are not interchangeable. The [API index](api.md) compares supported operations.

Verified against repository routes on 2026-10-02. Deployment availability depends on rebuilding and upgrading your installation. Only `v1` is implemented; unversioned application endpoints and unsupported versions are not aliases for the latest API.

## Entry points

| Interface | Base path | Authentication | Intended use |
| --- | --- | --- | --- |
| Studio and management | `/api/v1/studio` | Verified human access token for Studio | Author, publish, test, operate runs and manage authorized organizations/teams |
| Public startup | `/api/v1/health`, `/api/v1/auth/config` | None | Dependency health and browser login configuration |

Use Swagger UI at `/api/docs/studio/` on the running API/Studio origin. The machine-readable [Studio OpenAPI JSON](../server/workflows/internal/api/studio.openapi.json) is served at `/api/v1/studio/openapi.json`; this prose guide supplements it. Documentation is public, but Try it out requires appropriate human credentials. Keycloak's `/auth` endpoints belong to the identity provider, not Runloom. Infrastructure probes `/healthz` and worker `/readyz` are intentionally unversioned.

Use the same origin as Studio, normally `http://localhost:4200` for a forwarded local Kubernetes installation. Compose/custom installations may use another port. The Studio proxy forwards `/api` to the Go backend. Use HTTPS outside local development. See [deployment](deployment.md#development-cluster) and [API version policy](workflow-api.md#api-versions).

## Authentication and headers

Studio users sign in interactively through Keycloak. Runloom does not provide a password-login or password-reset API. Google login and password recovery are identity-provider functions. Send a short-lived access token, not an ID token. See [authentication](authentication.md).

| Header | When used | Meaning |
| --- | --- | --- |
| `Authorization: Bearer <access-token>` | Protected endpoints | Verified human token for Studio; service-account tokens are rejected |
| `Content-Type: application/json` | JSON request bodies | Submit one JSON value; typed request bodies reject unknown fields |
| `X-Runloom-Team: <team-id>` | Studio workflow/project/run operations | Select a team the human belongs to; not needed for `/me` or management routes that name their team in the URL |
| `X-Runloom-Edit-Token: <session-uuid>` | Studio edit leases and protected workflow writes | Identifies the editor session; never bypasses role checks |
| `X-Runloom-Project: <project-id>` | Studio draft save | Chooses the authoring folder; omission preserves membership or uses General for a new workflow |
| `X-Runloom-Commit-Message` | Studio draft/version operations | Optional commit description; see [workflow versioning](versioning.md) |

Actor identity comes from verified authentication, not a caller-supplied actor field/header. Project folders do not grant execution permissions. These rules describe the authenticated product gateway, not a directly exposed internal handler in a test fixture.

## Access by role

| Action in the selected team | Viewer | Editor | Team admin or organization owner/admin |
| --- | --- | --- | --- |
| List/read definitions, versions, projects, run status and history | Yes | Yes | Yes |
| Save, publish, restore, move workflows and acquire edit leases | No | Yes | Yes |
| Start main runs, test drafts, resume gates and cancel team runs | No | Yes | Yes |
| Deliver approvals, callbacks and named signals | No | Yes | Yes |
| Create/change schedules and project folders | No | Yes | Yes |
| Manage service accounts, credentials and secret/budget writes | No | No | Yes |

Studio access is team-wide, not restricted to runs started by the current person and not filtered by consumer grants. An editor can operate a consumer-started run recorded in their authorized team. Membership is checked on each request; cross-team run IDs return 404 before Temporal access. Organization owners/admins have effective admin access to their organization's teams. Ordinary organization membership does not grant access to every team. A team admin is not automatically an organization admin. See the management sections for path-specific permissions and [authentication](authentication.md) for membership rules.


## Studio workflow quick start

Examples use synthetic IDs and placeholders. Starting a run or draft test can send email, call external APIs or incur LLM charges; neither is a dry run.

1. Call `GET /api/v1/studio/me` with a human token to discover accessible organizations and teams. Select a team with editor/admin access.
2. Save and publish a main workflow using Studio or the definition endpoints below for production execution. Draft version `0` runs only through Studio's separate test endpoint. Acquire an edit lease before authoring an existing workflow, then reload its latest draft.
3. Start a published workflow using the human token and selected team.

```http
POST /api/v1/studio/runs HTTP/1.1
Host: localhost:4200
Authorization: Bearer <studio-access-token>
X-Runloom-Team: <team-id>
Content-Type: application/json

{"definition_id":"onboarding","version":1,"input":{"name":"Alex"}}
```

The HTTP 201 response includes `workflow_id`, `run_id`, `definition_id` and `definition_version`. Use `workflow_id` for subsequent Studio run URLs; `run_id` is the Temporal execution identifier. Omitting `version` selects the latest published definition. Studio starts do **not** have the consumer API's durable idempotency contract; do not blindly retry a lost response.

Read `GET /api/v1/studio/runs/{id}` for live status. Send `POST /api/v1/studio/runs/{id}/cancel` to request cancellation; HTTP 200 with `{"cancelled":true}` acknowledges that request, not completed cleanup. Full examples and response fields are in [workflow execution](workflow-api.md).

## Studio endpoint catalog

All paths in this section are relative to `/api/v1/studio`. Workflow/project/run routes require a team header. Team viewers can read; editors/admins can write. Organization owners/admins have effective team-admin access. Management routes enforce the organization/team named in their path and their own role rules.

### Definitions and publishing

| Method | Path | Request or result |
| --- | --- | --- |
| GET | `/catalog` | Available node/activity catalog |
| POST | `/definitions/validate` | Workflow JSON; validation result, no run |
| POST | `/definitions` | Workflow JSON; save draft and commit, 200 |
| GET | `/definitions` | Definition summaries; optional `project_id` |
| GET | `/definitions/{id}` | Definition JSON; optional `version`, including `0` for the draft |
| GET | `/definitions/{id}/history` | Published versions and commit history; optional bounded `limit` |
| GET | `/definitions/{id}/commits/{commit}` | Stored commit |
| POST | `/definitions/{id}/commits/{commit}/restore` | `{"message":"Restore reviewed design"}`; create a new draft, not overwrite a published version |
| POST | `/definitions/{id}/publish` | Publish an immutable version from the saved draft |
| POST | `/definitions/{id}/test` | `{"definition":<workflow JSON>,"input":{}}`; test a main/child draft snapshot without publishing, 201. Save a new workflow once first. Legacy `{"version":1,"input":{}}` tests published children only. See [test contract](workflow-api.md). |
| GET | `/definitions/{id}/edit-lock` | Safe owner/expiry status, never the lease token |
| POST | `/definitions/{id}/edit-lock` | Acquire a lease using `X-Runloom-Edit-Token` |
| PUT | `/definitions/{id}/edit-lock` | Renew that editor session's lease |
| DELETE | `/definitions/{id}/edit-lock` | Release that session's lease |
| POST | `/code/test` | JavaScript preview request; no workflow start |

Send the edit token on save, publish, restore and project moves. An active competing lease returns 423. An unlocked workflow can accept a tokenless legacy write under a one-operation reservation; that does not bypass an active lease. See [edit rules](workflow-editing.md#api-contract), [definition schema](../spec/workflow.schema.json), [commit/version API](versioning.md), and [JavaScript request and response contract](custom-code.md#studio-preview-api).

### Projects and schedules

| Method | Path | Request or result |
| --- | --- | --- |
| GET | `/projects` | Project array |
| POST | `/projects` | `{"name":"Finance"}`; created project, 201 |
| POST | `/projects/{project}/rename` | `{"name":"Operations"}`; renamed project, 200 |
| POST | `/definitions/{id}/project` | `{"project_id":"<destination-id>"}`; move within the team |
| GET | `/schedules` | Schedule array |
| POST | `/schedules` | Name, main definition ID, pinned version, input and one schedule expression; 201 |
| GET | `/schedules/{schedule}` | Schedule details, next runs and recent runs |
| POST | `/schedules/{schedule}/{action}` | `pause`, `resume`, `trigger` or `delete`; 200 |
| DELETE | `/schedules/{schedule}` | Delete the schedule; 200 |

Schedule bodies choose an interval, cron expression or one-time timestamp; see the exact fields and restrictions in [schedules](schedules-and-subworkflows.md). Triggering schedules and testing children cause real execution. Project moves do not change a published workflow or stop its runs. See [project contract](projects.md#api).

### Runs and workflow events

| Method | Path | Request or result |
| --- | --- | --- |
| POST | `/runs` | Start published main definition, 201 |
| GET | `/runs` | Run records; bounded `limit`, optional `include_usage=true` |
| GET | `/runs/{id}` | Live execution status/state |
| GET | `/runs/{id}/history` | `{run,events}` from application storage/audits, not raw Temporal replay history |
| GET | `/runs/{id}/llm-usage` | `{summary,entries,next_cursor,currency,scope}` |
| GET | `/audit` | Audit array; `workflow_id`, `definition_id`, bounded `limit` |
| POST | `/runs/{id}/approvals` | `{"step_id":"review","approved":true,"comment":"Reviewed"}` |
| POST | `/runs/{id}/callbacks` | `{"step_id":"callback","payload":{"reference":"example"}}` |
| POST | `/callbacks/{id}/{step}` | `{"payload":{"reference":"example"}}`; step comes from the URL |
| POST | `/runs/{id}/signals/{signal}` | `{"payload":{"ready":true}}`; named signal comes from the URL |
| POST | `/runs/{id}/resume` | `{"step_id":"paused-step","payload":{}}` |
| POST | `/runs/{id}/cancel` | Request cancellation, 200 |
| POST | `/webhooks/{definition}/{path}` | JSON input object; start the configured published main workflow |

Approvals, callbacks, signals and resumes return HTTP 200 with `{"accepted":true}` when the event is accepted. Acceptance does not mean the workflow has consumed it or completed. Despite the callback/webhook names, these routes are **not anonymous**: they require the human token, team scope and write permission. Cross-team run IDs are rejected before Temporal lookups/signals. There is no consumer event-delivery endpoint in v1. Named signals also target [independent Temporal handler paths](signal-handlers.md); an optional UUID `event_id` deduplicates handler invocations with the same sender and payload. Handler acceptance is not action completion.

A hard LLM budget limit pauses a workflow rather than completing/cancelling it. Resume must identify the paused step and will recheck the applicable budget. See [events and control semantics](workflow-api.md), [JSON node contracts](json-contract.md), and [LLM usage and budgets](llm-costs.md).

### Stop and resume semantics

There is no general-purpose `/stop`, `/pause` or `/terminate` run endpoint. To stop permanently, request cancellation; this does not undo completed side effects and a canceled run cannot be resumed. To wait and later continue, design a Pause node into the workflow. Pausing a **schedule** prevents future scheduled starts; it does not pause an existing execution.

An editor/admin resumes an existing pause using its run ID and the step reported in `state.paused_step` or `state.paused_steps`:

```http
POST /api/v1/studio/runs/<workflow-id>/resume HTTP/1.1
Authorization: Bearer <studio-access-token>
X-Runloom-Team: <team-id>
Content-Type: application/json

{"step_id":"review-pause","payload":{"note":"Ready to continue"}}
```

HTTP 200 with `{"accepted":true}` acknowledges signal delivery, not a confirmed state transition. The handler validates the step ID but does not verify that the run is currently paused at that step; an early resume can be buffered. Read current state first, target the correct run/step, and poll afterward. Do not use resume to complete an approval, callback or named-signal gate. Each has its own endpoint. A child's pause must be addressed to the child run. A budget pause rechecks the policy and may remain paused. Detailed examples are in [Studio run controls](workflow-api.md#4-resume-a-paused-workflow).

### Organizations and teams

| Method | Path | Request or result |
| --- | --- | --- |
| GET | `/me` | Human profile and accessible organizations/teams |
| POST | `/orgs` | `{"name":"Example organization"}`; 201 with `{id}` |
| POST | `/orgs/{org}/teams` | `{"name":"Operations"}`; 201 with `{id}` |
| GET | `/orgs/{org}/members` | Organization members |
| POST | `/orgs/{org}/members` | `{"email":"person@example.test","role":"member"}` |
| DELETE | `/orgs/{org}/members/{email}` | Remove membership; 200 with `{removed:true}` |
| GET | `/orgs/{org}/teams/{team}/members` | Team members |
| POST | `/orgs/{org}/teams/{team}/members` | `{"email":"person@example.test","role":"editor"}` |
| DELETE | `/orgs/{org}/teams/{team}/members/{email}` | Remove team membership; 200 with `{removed:true}` |
| GET | `/orgs/{org}/audit` | Authorized organization audit records |

Organization roles are owner/admin/member; team roles are admin/editor/viewer. Role changes require management permission and preserve last-owner protections. URL-encode email addresses and other path parameters. See [identity and membership rules](authentication.md).

### Credentials and cost management

| Method | Path | Request or result |
| --- | --- | --- |
| GET | `/orgs/{org}/teams/{team}/service-accounts` | Account metadata, never client secrets |
| POST | `/orgs/{org}/teams/{team}/service-accounts` | Policy/grants; 201 with account, client ID and one-time secret |
| PUT | `/orgs/{org}/teams/{team}/service-accounts/{account}` | Update policy; 200 with `{updated:true}` |
| POST | `/orgs/{org}/teams/{team}/service-accounts/{account}/{action}` | `rotate` or `revoke`; rotation reveals a replacement secret once |
| GET | `/orgs/{org}/teams/{team}/secrets` | Configuration state and secret metadata; never secret values |
| PUT | `/orgs/{org}/teams/{team}/secrets/{name}` | `{"value":"<secret>"}`; no allowed-origin field |
| DELETE | `/orgs/{org}/teams/{team}/secrets/{name}` | Remove stored secret |
| GET | `/orgs/{org}/llm-analytics` | Authorized spend KPIs, trends and paginated/sortable rows |
| GET | `/orgs/{org}/teams/{team}/llm-budget-policies` | Policies, project spend, alerts and changes |
| PUT | `/orgs/{org}/teams/{team}/llm-budget-policies/{policy}` | Save policy with optimistic revision checks |

Service-account management and secret/policy writes require effective team-admin access. Secret metadata and budget reads are available to authorized team members. Analytics only aggregates authorized teams. Credentials must not enter logs, source, screenshots or browser persistence. See the policy schema below, [workflow secrets](api-nodes-and-secrets.md), and [cost analytics and budgets](llm-costs.md).

### Service account policies

The `{account}` path value is the account's `client_id`. Create/update bodies use this policy shape:

```json
{
  "name": "Billing integration",
  "scopes": ["workflows:read", "runs:start", "runs:read", "runs:cancel"],
  "requests_per_minute": 60,
  "max_concurrent_runs": 5,
  "grants": [{"definition_id":"invoice","versions":[1],"input_fields":[],"output_fields":["receipt_id"]}]
}
```

Only those four scopes are supported; `runs:resume`, `runs:pause` and `runs:stop` are not implemented. Policies require 1–100 workflow grants, each with 1–100 explicit published versions; drafts and child definitions are rejected. Request rates are 1–10,000 per minute; maximum concurrent root runs are 1–100. Exposure fields name top-level fields; `*` is forbidden. Exposure controls responses, not the input schema accepted when starting a workflow.

Create and rotate return a one-time `client_secret`; list and policy-update responses never return it. Store it in the integrating backend's secret manager, never browser persistence. Scopes are immutable after creation: policy updates must retain the existing scope set, and adding or removing scopes returns 422. Create a new account to use different scopes. Rotation retains the client ID/run ownership but invalidates the previous credential generation. Revocation blocks subsequent requests without canceling running workflows. See [operator provisioning and credential lifecycle](consumer-api.md#team-administrator-workflow).

## Errors and retries

Studio handler errors normally use `{"detail":"message"}`. A 423 editing response also includes `error` and safe `lock` metadata. Router-level unmatched paths/methods may use a plain-text response; check the HTTP status and content type before decoding. Consumer errors use a different envelope documented in their separate guide.

| Status | Meaning and caller action |
| --- | --- |
| 400 / 422 | Invalid request/filter/schema; correct it before retrying |
| 401 | Missing, expired, revoked or wrong-kind token; authenticate appropriately |
| 403 | Insufficient role or team membership; do not switch credentials to bypass restrictions |
| 404 | Missing/inaccessible resource or unsupported endpoint/version |
| 409 | Conflict, including a stale policy revision |
| 423 | Active competing edit lease; show owner/status and wait or release the correct session |
| 500 / 502 / 503 | Storage/dependency/dispatch problem; acceptance may be uncertain, so reconcile before retrying mutations |

Studio start, draft test, webhook and schedule-trigger operations have no durable start-request idempotency guarantee. Adding an `Idempotency-Key` header does not provide consumer-style protection here. Reconcile uncertain outcomes before retrying mutations. Cancellation is cooperative and asynchronous; resume acceptance does not guarantee continuation. The API does not promise exactly-once external activity side effects.

Do not assume every list is cursor-paginated. Consumer lists use `items/next_cursor`; Studio run/audit lists use bounded arrays. LLM attempts use `entries/next_cursor`; analytics uses pages. Inspect each linked endpoint contract before implementing pagination or polling.

## Source and further reading

The checked implementation is in [Studio routes](../server/workflows/internal/api/server.go), [gateway and management routes](../server/workflows/internal/api/gateway.go), and [service-account management](../server/auth/internal/api/service_accounts.go). The [workflow schema](../spec/workflow.schema.json) defines workflow JSON, including optional saved canvas layout. Regression evidence lives in [team/role tests](../server/workflows/internal/api/gateway_test.go), [consumer boundary tests](../server/workflows/internal/api/consumer_test.go), and [API version routing tests](../server/workflows/internal/api/api_version_test.go). This documentation adds no endpoints, permissions, credentials or deployment changes.
