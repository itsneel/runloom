# Schedules and reusable subworkflows

Schedules and subworkflows use native Temporal Schedules and Child Workflows. There is no browser timer or API cron process to keep running. The API resolves published definitions before execution; workers never fetch mutable definitions from inside workflow code.

## Builder walkthrough

The [runnable sample pack](../examples/README.md) includes ready-to-use workflow JSON, sample inputs, hourly/weekday requests, a one-time request generator, and publishing/testing instructions.

For a parallel child-workflow demo, open **Workflows**, select **Example → Parallel child workflows**, and choose **Try a guided example**. Confirm creation of the published demo child and parent draft, then choose **Test workflow → Start new test** with the prefilled input. The two child timers run together, their results join, and an End node completes the parent. No external integrations are called. See the [sample files and expected output](../examples/README.md#try-parallel-child-workflows).

Manual imports use [parallel-greeting.json](../examples/parallel-greeting.json) and [parent-with-parallel-subworkflows.json](../examples/parent-with-parallel-subworkflows.json).

1. Create a workflow in any project, set **Workflow kind → Child — parent calls only**, save it as a draft and use **Test child workflow** without publishing. Publish it when a parent needs to reference it. Tests execute real activities, not a sandbox.
2. Open a parent workflow, choose **Add node → Subworkflow**, select the published child, and choose its exact version. Map input using JSON values and references such as `${input.customer}`. The parent reads the child's result through `${steps.call-child.output}`.
3. Save and publish the parent. Missing child versions, cross-team references, and circular dependencies are rejected. You can reuse workflows across projects within the same team; projects remain authoring folders, not execution boundaries.
4. Open **Management → Schedules → New schedule**. Select a published main workflow/version and choose an interval, five-field calendar cron with an IANA time zone, or a one-time local date/time. Supply saved input and optionally create the schedule paused. The builder's **Manage schedules** shortcut opens this Management page and checks for unsaved workflow edits first.
5. Search, filter by active/paused status, or sort by name. Use **Pause**, **Resume**, **Run now**, **Delete**, and **Refresh schedules** to manage team schedules. Recent-run links open the separate Management run-history dashboard. Unsaved schedule edits are protected when closing the editor, switching teams, navigating away, signing out, or closing the browser.

## Execution rules

| Area | Rule |
| --- | --- |
| Workflow kind | `kind: "main"` (the default) can start through the API, webhooks, schedules or Studio testing. `kind: "child"` requires a parent's Subworkflow node for production, but child drafts may be tested independently in Studio. Child workflows may themselves call other children. |
| Enforcement | Normal child start and schedule requests return HTTP 422. Child webhook definitions are rejected. The normal worker entry point checks Temporal's actual parent identity, not a supplied parent ID or test flag. Studio tests use a separate authenticated action and worker entry point. Child status, callbacks, signals, resumes and cancellation remain available through the API. |
| Version pinning | A schedule stores the selected published parent and all transitive child snapshots when created. Manual and webhook starts resolve pinned child versions when starting. Publishing a newer version never changes an existing schedule or execution. |
| Updating a schedule | Delete and recreate it to change version, input, or timing. In-place editing, backfill, and configurable overlap policies are not implemented. |
| Frequency | Intervals are 60–31,536,000 seconds, aligned to Temporal's interval clock, not a countdown from creation. Calendar expressions contain minute, hour, day, month, weekday. A one-time `at` is a future RFC3339 timestamp; the builder converts local browser time to UTC. |
| Calendar time | Time zones are validated IANA names; UTC is the API default. Daylight-saving changes can skip or repeat local wall-clock times. Use UTC when that is undesirable. |
| Overlap | Skip a new scheduled start while the previous run is active, including a parent waiting for a child. Manual Run now uses this same policy. |
| Missed starts | Temporal may catch up starts within a 15-minute window. Older missed occurrences are skipped. This does not guarantee exactly-once external side effects. |
| Schedule failure | Failed or timed-out runs pause future scheduled starts. Review the failure before resuming. |
| Pause/delete | Stops future starts only. Already-running executions continue. Run now is allowed on a paused schedule. Use run cancellation to stop an execution. |
| One-time schedules | One automatic occurrence, subject to the same catch-up window. A paused one-time schedule can miss its date. Temporal may eventually remove exhausted schedules; application run history remains. |
| Child completion | The parent waits. The child's declared `output` becomes the Subworkflow node output. Child input is schema-validated before any business step executes. |
| Child failure | Failure or timeout fails the parent. Child workflow execution is not automatically retried; activities inside it retain their configured retries. |
| Child lifetime | `timeout_seconds` defaults to 3,600 and accepts 1–31,536,000. Parent cancellation requests child cancellation; parent closure terminates any child still running. |
| Callbacks and pauses | Address signals, callbacks, approvals and resume requests to the child run ID. They are not forwarded from the parent. Nested waits survive worker restarts through Temporal history. |
| Limits | Maximum eight child levels, 100 distinct pinned dependency versions, and 1 MiB for root definition, dependency snapshots and initial input. Existing per-definition limits still apply. Intermediate payloads remain subject to Temporal limits. |
| Permissions | Team viewers may read; editors/admins may mutate or trigger schedules. No cross-team child calls. Schedules are team-owned automation and continue if their creator leaves the team. |

Schedules are stored in Temporal, not the project catalog. Metadata includes saved input; do not place secrets in it. Listing uses eventually consistent Temporal visibility and filters by team before returning metadata. Refresh may briefly lag changes. The endpoint scans at most 10,000 namespace schedules and returns HTTP 503 rather than a partial list if that limit is exceeded.

## Subworkflow JSON

Publish [reusable-greeting.json](../examples/reusable-greeting.json) before saving/publishing [parent-with-subworkflow.json](../examples/parent-with-subworkflow.json). The child node is:

```json
{
  "id": "call-greeting",
  "name": "Prepare greeting",
  "type": "subworkflow",
  "definition_id": "reusable-greeting",
  "version": 1,
  "with": { "name": "${input.name}" },
  "timeout_seconds": 3600
}
```

## Schedule API

Requests require `Authorization: Bearer <access-token>` and `X-Runloom-Team: <team-uuid>`, like the [run API](workflow-api.md). No project ID is required. See [schedule.schema.json](../spec/schedule.schema.json).

| Method | Route | Result |
| --- | --- | --- |
| POST | `/api/v1/studio/schedules` | Create; HTTP 201 with metadata and ID. |
| GET | `/api/v1/studio/schedules` | List team schedules. |
| GET | `/api/v1/studio/schedules/{id}` | Configuration, pause state, next times and recent run IDs. |
| POST | `/api/v1/studio/schedules/{id}/pause` | Pause future starts. |
| POST | `/api/v1/studio/schedules/{id}/resume` | Unpause future starts. |
| POST | `/api/v1/studio/schedules/{id}/trigger` | Request a run using saved input, even if paused; overlap may skip it. |
| DELETE | `/api/v1/studio/schedules/{id}` | Delete the schedule, preserving executions and run records. |

Example create body:

```json
{
  "name": "Weekday greeting",
  "definition_id": "parent-with-subworkflow",
  "version": 1,
  "input": { "name": "Avery" },
  "cron": "0 9 * * 1-5",
  "time_zone": "Asia/Kolkata",
  "paused": false
}
```

Specify exactly one of `cron`, `interval_seconds`, or `at`. Version 0 and implicit latest versions are not allowed. `time_zone` affects calendars; `at` carries its own offset. Invalid JSON is HTTP 400, invalid configuration 422, inaccessible/missing schedules 404, insufficient roles 403, and Temporal service errors 503. Mutations return HTTP 200 with `schedule_id` and `action`, not a run ID. Read `recent_runs` or team history to find executions.

Run status includes `state.child_workflows` (node ID to child run ID), `state.parent_workflow_id` for children, and `state.schedule_id` for scheduled roots. Scheduled and child records have `trigger_type` values `schedule` and `child`. Draft tests have `trigger_type: "draft_test"`; legacy published child tests and nested test children use `child_test`. All have `state.is_test: true`. Original input, status/output and `workflow.started` audits use the selected file/PostgreSQL/MongoDB store. Schedule mutations also emit audits. Status snapshots use existing API reconciliation; Temporal history is authoritative.

## Independent child tests

An editor/admin can open a main or child draft and choose **Test workflow** or **Test child workflow**, enter input, and select **Start new test**. Publishing is not required. Save a brand-new workflow once to establish its team/project identity. Studio submits the current builder snapshot, including unsaved edits; testing does not save, commit or publish. Later edits do not change an active test. Referenced subworkflows still use their pinned published versions; the root test has no parent. Normal API, webhook and schedule starts of child definitions remain blocked. Neither a caller-supplied parent ID nor an `is_test` flag bypasses the normal start API or worker guard.

Studio calls `POST /api/v1/studio/definitions/{id}/test` with `definition` (the complete current JSON snapshot) and `input`. The route ID must match an existing team-local draft. The older published `version` request remains supported for child tests only. The gateway applies the same authenticated team/editor permissions as other mutations. Clients with valid editor credentials can also call this endpoint; HTTP cannot securely distinguish a Studio click from the same authorized request made elsewhere. This is a separate test capability, not a UI-origin security boundary.

The endpoint validates and normalizes the draft snapshot to version zero, checks its input schema and resolves dependencies before starting `RunloomDraftTestWorkflow`. Legacy published child tests retain `RunloomChildTestWorkflow`. The normal `RunloomWorkflow` entry point still requires a real Temporal parent for a child. Tests execute real activities, including HTTP/LLM requests and external side effects; they are not sandboxed. Nested subworkflows keep normal parent/child lifetime rules and inherit the test label. Use the test's run ID for callbacks, signals, approvals, pause/resume and cancellation. Input, output and audits remain visible in normal run history, marked as tests.

## Deployment and verification

API and worker must share the same Temporal namespace/task queue **and** database driver, connection, database name and, for native filesystem development, data directory. Workers register scheduled/child runs in the team store before user nodes execute. Registration retries for up to five minutes on storage failure; no user node executes until it succeeds. Invalid child input is recorded and fails without running business steps. Compose and Helm pass database settings to both processes. Worker readiness checks Temporal and database connectivity.

Deploy upgraded workers before enabling child nodes, independent child tests or creating schedules. Deploy workers registering `RunloomDraftTestWorkflow` before updating the API/Studio for draft tests. Keep `RunloomChildTestWorkflow` and `RunloomWorkflow` registered for their existing executions. Rolling back to a worker that does not understand these inputs is unsafe. Existing manual inputs do not emit the new registration command. Definitions without `kind` remain main workflows for compatibility. Existing published versions and already-running snapshots are not rewritten. To convert a previously reusable main definition, publish a new version with `kind: "child"`, then update and republish parents to pin that child version. Recreate affected schedules to use the new parent release. Old main versions remain main versions; new child-only versions cannot be started through normal production endpoints. For busy multi-tenant deployments, evaluate supported per-team task-queue fairness and capacity limits to prevent starvation; this change does not enable fairness or provision infrastructure. Keep activity side effects idempotent and back up both Temporal and the application database.

Run `make test`, `make check-go`, `make docker-config`, and `make helm-lint`. For synthetic end-to-end verification start an isolated server:

```bash
temporal server start-dev --ip 127.0.0.1 --port 27234 --ui-port 28234
```

In another terminal:

```bash
cd server/workflows
RUNLOOM_TEST_TEMPORAL_ADDRESS=127.0.0.1:27234 go test ./internal/api -run TestSchedulesAndChildrenLive -count=1 -v
```

This covers creation, triggering, skip-overlap, automatic one-time execution, pause/resume/delete, pinned children, mapped input, callbacks, history/audits, cancellation, invalid input, independent Studio child tests, rejection of normal child starts, and parent/child/test replay. Optionally set `RUNLOOM_TEST_DATABASE_DRIVER` and `RUNLOOM_TEST_DATABASE_URL` to a disposable PostgreSQL/MongoDB instance. It writes synthetic records under a random team; use disposable storage, never business databases.

The browser regression scripts `scripts/schedules-dashboard-smoke.mjs` and `scripts/unsaved-changes-smoke.mjs` test Management scheduling, form failures, pause/resume, navigation guards and Subworkflow controls against mocked APIs. See the scripts for Playwright/browser environment overrides; use the three independent Angular development servers. These UI checks do not execute real Temporal schedules.

Upstream behavior: [Temporal Go schedules](https://docs.temporal.io/develop/go/schedules), [Temporal child workflows](https://docs.temporal.io/child-workflows).
