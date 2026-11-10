# Runnable workflow samples

For a JavaScript-only transformation, import [javascript-transform.json](javascript-transform.json) and use [order.json](inputs/order.json). It returns `{"order":{"total":40,"itemCount":2}}` without external integrations. See [Custom Code](../docs/custom-code.md) for the editor and runtime limits.

The greeting and schedule examples below use only the built-in echo action. They do not send email, call an LLM, or need integration credentials. They create real execution records when you run them. Child versions are explicitly pinned; schedule timing is separate from workflow JSON.

For Email, import [email-notification.json](email-notification.json) with `{"email":"recipient@example.test","name":"Runloom"}`. This executes a real SMTP send; default development Compose captures it in Mailpit. See [Email setup](../docs/email-node.md) before using another SMTP server.

## Choose a sample

| Sample | Purpose | Test input | Expected output |
| --- | --- | --- | --- |
| [reusable-greeting.json](reusable-greeting.json) | Child-only greeting (`kind: "child"`); invoke through either parent below, or test independently in Studio. | [greeting.json](inputs/greeting.json), mapped by the parent or supplied as Studio test input. | `{"message":"Hello, Avery!"}` |
| [parent-with-subworkflow.json](parent-with-subworkflow.json) | Parent maps its input into child version 1 and returns its output. | [greeting.json](inputs/greeting.json) | `{"message":"Hello, Avery!"}` |
| [parent-with-two-subworkflows.json](parent-with-two-subworkflows.json) | Reuse the same child twice, sequentially, with different inputs. | [customer-and-reviewer.json](inputs/customer-and-reviewer.json) | `{"customer":"Hello, Avery!","reviewer":"Hello, Jordan!"}` |
| [parallel-greeting.json](parallel-greeting.json) | Demo child: wait five seconds and return a greeting. | [greeting.json](inputs/greeting.json) | `{"message":"Hello, Avery!"}` |
| [parent-with-parallel-subworkflows.json](parent-with-parallel-subworkflows.json) | Two child runs start together, join their results, then End. | [customer-and-reviewer.json](inputs/customer-and-reviewer.json) | `{"customer":"Hello, Avery!","reviewer":"Hello, Jordan!"}` |
| [hourly-greeting.json](schedules/hourly-greeting.json) | Run the single-child parent every hour. | Saved inside the schedule. | Single greeting. |
| [weekday-greetings.json](schedules/weekday-greetings.json) | Run the two-child parent at 9am Monday–Friday in Asia/Kolkata. | Saved inside the schedule. | Customer and reviewer greetings. |
| [one-time-greeting.mjs](schedules/one-time-greeting.mjs) | Generate a fresh one-time schedule request for five minutes from now. | Saved inside the generated request. | Single greeting. |

All schedule samples start **paused** so importing one does not enable recurring execution. Use Run now for an immediate test, or Resume to enable its timetable. The one-time sample is generated rather than hard-coded so its date does not expire. Resume it before its chosen time; generate another request if you miss it.

## Try parallel child workflows

1. In Studio's **Workflows** dashboard, select **Example → Parallel child workflows**, then **Try a guided example**.
2. Select **Create example**. This saves and publishes a new demo child and saves a parent draft in the selected project. Unique IDs preserve existing workflows. Nothing runs yet; only local greetings and timers are used, with no API credentials or LLM calls.
3. Choose **Test workflow**, then **Start new test**. The input is prefilled with `{"customer":"Avery","reviewer":"Jordan"}`. Both child runs wait five seconds together; the parent joins their greetings and ends. Worker startup/queueing may add time. Inspect both child IDs in run state/history. The parent does not need publishing.

To import the JSON files manually, save and publish `parallel-greeting.json` first, then save `parent-with-parallel-subworkflows.json` and test it with `inputs/customer-and-reviewer.json`. Its child calls pin version 1; update those pins if using another published child version. Child failures cancel the remaining parallel work and fail the parent. End is after the join, never inside a branch.

## Try it in the Studio

1. Start the complete stack using the repository's [setup instructions](../README.md#quick-start), sign in, and choose a team where you are an editor/admin. Use a fresh sample project or team to avoid replacing drafts with matching sample IDs.
2. Create a workflow, open **JSON**, paste `reusable-greeting.json` (which declares `kind: "child"`), apply, save, and publish. On a fresh ID, this creates version 1. Do this before importing either parent. To test independently, click **Test child workflow**, paste `inputs/greeting.json`, and start the test. Expect `{"message":"Hello, Avery!"}` and a test label in history. Normal API and schedule starts of the child remain rejected; Studio tests use a separate authenticated test action and run real activities.
3. Import, save and publish `parent-with-subworkflow.json` and optionally `parent-with-two-subworkflows.json` the same way. Every node pins child version 1. If that ID already exists, verify version 1 is the expected child-only greeting. If an older version was published as a main workflow, publish a new child-only version and update the parent pins, or use fresh IDs throughout. Published versions are never rewritten.
4. Click **Test workflow** on either parent and paste its test input from the table. Open each child from the run panel and check its input and output. The parent's result should match the table.
5. Open **Schedules**, select the published parent/version and copy the timing/input settings from the relevant schedule sample. For hourly, choose an interval of 60 minutes. For weekday, select the weekday 9am preset and `Asia/Kolkata`. For once, pick a future local date/time. Check **Create paused**, create, then use **Run now** or **Resume**.

Schedule request JSON belongs to `/api/v1/studio/schedules`, not the workflow JSON editor. The Studio has a schedule form, not a schedule-JSON import field. Child definitions can live in any project in the same team. Publishing child version 2 will not update these version-1 parents or existing schedules.

## Run the samples through the API

Use Bash with curl and jq, from the repository root. Follow [authentication setup](../docs/workflow-api.md#authentication-and-setup) to put your `Authorization` and `X-Runloom-Team` headers in a private file outside the repository. Do not paste tokens into commands or commit them.

```bash
set -euo pipefail
RUNLOOM_BASE_URL=http://localhost:4200
RUNLOOM_AUTH_HEADERS_FILE=/absolute/private/runloom-headers
chmod 600 "$RUNLOOM_AUTH_HEADERS_FILE"

sample_api() {
  local method="$1" path="$2"
  shift 2
  curl --silent --show-error --fail-with-body \
    --request "$method" "$RUNLOOM_BASE_URL$path" \
    --header @"$RUNLOOM_AUTH_HEADERS_FILE" \
    --header 'Content-Type: application/json' "$@"
}
```

In a fresh sample team, save and publish in dependency order. These commands intentionally write sample definitions. Repeating them creates additional draft commits and published versions; they do not overwrite published versions. Stop if any command fails.

```bash
for sample_id in reusable-greeting parent-with-subworkflow parent-with-two-subworkflows; do
  sample_api POST /api/v1/studio/definitions --data-binary "@examples/$sample_id.json"
  sample_api POST "/api/v1/studio/definitions/$sample_id/publish" --data '{}'
done
```

Start the two-child parent and inspect the returned ID:

```bash
SAMPLE_RUN_ID="$(jq -c '{definition_id:"parent-with-two-subworkflows",version:1,input:.}' \
  examples/inputs/customer-and-reviewer.json | \
  sample_api POST /api/v1/studio/runs --data-binary @- | jq -er '.workflow_id')"
sample_api GET "/api/v1/studio/runs/$SAMPLE_RUN_ID"
sample_api GET "/api/v1/studio/runs/$SAMPLE_RUN_ID/history"
```

Status may initially be running. Repeat the status request until completed; `state.child_workflows` links to the two child runs and `state.output` contains their combined result.

Create a paused hourly schedule. To use the weekday sample instead, replace the file path with `examples/schedules/weekday-greetings.json`:

```bash
SAMPLE_SCHEDULE_ID="$(sample_api POST /api/v1/studio/schedules \
  --data-binary @examples/schedules/hourly-greeting.json | jq -er '.id')"
sample_api POST "/api/v1/studio/schedules/$SAMPLE_SCHEDULE_ID/trigger" --data '{}'
sample_api GET "/api/v1/studio/schedules/$SAMPLE_SCHEDULE_ID"
```

Trigger uses saved input even while paused. It requests execution; it does not return a run ID or wait for completion. Read `recent_runs` in the schedule response after the run starts. Overlapping starts are skipped.

To create a fresh one-time request, Node.js is needed only for this generator. Running it alone prints JSON and changes nothing on the server:

```bash
node examples/schedules/one-time-greeting.mjs
```

Generate, create, and resume a one-time schedule ten minutes in the future:

```bash
SAMPLE_ONCE_ID="$(node examples/schedules/one-time-greeting.mjs 10 | \
  sample_api POST /api/v1/studio/schedules --data-binary @- | jq -er '.id')"
sample_api POST "/api/v1/studio/schedules/$SAMPLE_ONCE_ID/resume" --data '{}'
sample_api GET "/api/v1/studio/schedules/$SAMPLE_ONCE_ID"
```

Optional recurring-schedule controls, using the hourly schedule ID from above:

```bash
sample_api POST "/api/v1/studio/schedules/$SAMPLE_SCHEDULE_ID/resume" --data '{}'
sample_api POST "/api/v1/studio/schedules/$SAMPLE_SCHEDULE_ID/pause" --data '{}'
sample_api DELETE "/api/v1/studio/schedules/$SAMPLE_SCHEDULE_ID"
```

Delete only the sample schedule IDs you created. Each create request makes a distinct schedule, even with the same name. Deleting or pausing does not cancel runs already started or delete history. Full [schedule and child-workflow rules](../docs/schedules-and-subworkflows.md) explain version pinning, time zones, cancellation, persistence and production fairness considerations.

## Check sample files

```bash
node --test examples/schedules/one-time-greeting.test.mjs
cd server/workflows
go test ./internal/definition ./internal/api ./internal/engine
```

These checks validate the static JSON examples against the schemas and backend rules and execute the parent/child fixtures in Temporal's local test environment. They do not create live schedules or alter application data.
