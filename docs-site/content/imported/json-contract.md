# JSON contract version 1.0

The authoritative structural contract is `spec/workflow.schema.json`.

## Canvas layout

An optional `layout.positions` object stores Studio coordinates, for example `{"layout":{"positions":{"prepare":{"x":300,"y":-120}}}}`. Keys are node IDs, `@trigger`, `@finish`, or `@join:<node-id>`. There are at most 1,002 entries; coordinates are finite numbers from -100,000 to 100,000. This metadata is preserved in draft saves, commits and published snapshots by the existing file, PostgreSQL and MongoDB definition stores. It has no effect on execution: `steps`, `then`, `else` and `branches` still determine order. Older definitions do not need this field. Pan and zoom are not stored in workflow JSON.

## Value references

Strings can refer to run data with `${...}` placeholders. A string containing only one placeholder preserves the referenced JSON type. A placeholder embedded in other text is converted to text.

1. `${input.employee.name}` reads workflow input.
2. `${steps.prepare.output.message}` reads an earlier step output.
3. `${steps.manager-approval.output.approved}` reads an approval decision.

Missing paths fail the run instead of silently producing empty values. Within a parallel branch, references may use inputs, pre-existing steps, and earlier steps in the same branch. They cannot depend on sibling branches.

## Triggers

`kind` is `main` by default, or `child` for parent-only production execution. Child definitions cannot configure a webhook trigger. Their manual/default trigger is just a schema placeholder, not permission for normal API starts. Studio editors/admins may independently test a published child through the separate authenticated test endpoint; tests execute real activities and are labeled in history. Normal API, webhook and schedule starts stay blocked. Main workflows cannot be selected as subworkflow targets. The kind is part of each immutable published version.

A missing trigger normalizes to `{ "type": "manual" }`. A manual trigger starts from the run API. A webhook trigger contains a restricted `path` and `method: "POST"`; posting a JSON object to `/api/v1/studio/webhooks/{definition-id}/{path}` starts the latest published version and uses that object as workflow input.

## Step types

### Database

A `database` node references a saved team connection and supports guided reads/writes, bound SQL queries, and MongoDB JSON aggregation. Credentials stay outside workflow definitions. See [Database connections](database-connections.md) for schema discovery, mappings, permissions, configuration and uncertain-write handling. The node returns `rows`, `affected` and `truncated` and uses one attempt with a fixed timeout.

### Email

An `email` node contains an `email` object with required `to` (address array), `subject` and `text`, plus optional `cc`, `bcc`, `reply_to` and `html`. Fields support interpolation and are revalidated before SMTP delivery. Delivery has one attempt with system-managed timeouts; author-supplied `with`, retry and timeout fields are rejected. See [Email](email-node.md) for limits, credential configuration and uncertain-delivery rules.

AI / LLM execution records attempt-level token usage and estimated costs without adding pricing or credentials to workflow JSON. See [run cost accounting](llm-costs.md).

### Custom Code (JavaScript)

A `code` node contains `code.runtime: "quickjs-ng-0.11.0-v1"`, literal `code.source` and a mapped `with` object. Its isolated Activity receives that object as `input` and returns JSON. JavaScript only; no TypeScript, npm, direct network or secret access. Timeout and retry fields are not configurable on this node. See [Custom Code](custom-code.md) for resource limits, testing and security rules.

### Subworkflow

A `subworkflow` node names a same-team published `definition_id` and explicit `version` >= 1. Its `with` object maps parent values into child input, and the child's declared output becomes the node output. `timeout_seconds` defaults to 3600. The parent waits for the child; failure propagates, and cancellation closes active children. Calls use embedded immutable snapshots, not database lookups during replay. See [schedules and subworkflows](schedules-and-subworkflows.md) for limits and lifetime rules. Schedules are separate configuration in `spec/schedule.schema.json`, not an extra workflow trigger type.

### Activity

An activity invokes a capability registered on the worker. `with` is resolved immediately before invocation. `timeout_seconds` is required by the runtime and defaults to 60 during normalization. Retry values map to Temporal activity retry policy.

### AI / LLM

An LLM node resolves its system prompt and prompt from workflow data, then calls the registered `runloom.llm` activity. The node supports model, temperature, maximum tokens, timeout, and retry policy. The built-in activity uses an OpenAI-compatible chat-completions endpoint. Provider credentials and defaults are worker configuration and must never appear in a definition.

Every newly executed built-in LLM call, including Action nodes invoking `runloom.llm`, requires a strict JSON response envelope: `{"content":"The answer","hitl_required":false,"hitl_reason":""}`. `hitl_required` means human-in-the-loop review is required; it must be an explicit boolean. When true, `hitl_reason` must contain an actionable explanation of 1–2000 bytes; when false it must be empty. Missing, null, wrongly typed or duplicate fields, unknown fields, refusals and truncated responses fail the node without automatic validation retries. Reported provider usage is still accounted for. Provider/model support for strict JSON Schema output is now required for ordinary LLM calls too; no free-text fallback is used. The format follows [structured-output guidance](https://developers.openai.com/api/docs/guides/structured-outputs?api-mode=chat) and is independently validated by Runloom.

The node unwraps the answer into its existing `content` field and adds `hitl_required` and `hitl_reason` alongside `model`, `usage` and accounting metadata. Use `${steps.ask.output.hitl_required}` in a Condition leading to Approval. Ordinary LLM nodes expose the flag without introducing an automatic pause; existing AI Decision handoff behavior is described below. The model's assessment is not authorization and is not a substitute for mandatory approval policies. Previously recorded LLM results replay unchanged and are not retroactively assigned a review assessment.

### Typed prompts and prompt library

LLM and AI Decision nodes support either legacy `prompt`/`system_prompt` strings or `prompt_template` with separately mapped `prompt_inputs`. Do not combine them. Templates use only declared `${vars.name}` placeholders; workflow expressions belong in the mappings. Supported variable types are string, number, integer, boolean, object and array. Optional variables require a typed default. Missing values, wrong types and undeclared inputs are rejected before a provider call. Objects and arrays render as JSON. Substitution is single-pass: expressions inside an inserted value are not executed.

```json
{
  "id": "summarize",
  "type": "llm",
  "model": "default",
  "prompt_template": {
    "instructions": "Summarize the supplied customer request.",
    "text": "Customer: ${vars.customer}. Request: ${vars.request}",
    "variables": [
      {"name": "customer", "type": "string", "required": true},
      {"name": "request", "type": "object", "required": true}
    ]
  },
  "prompt_inputs": {"customer": "${input.name}", "request": "${input.request}"}
}
```

Prompt-library entries support up to 20 tags of 40 characters each. Enter comma-separated tags in the editor; they are trimmed, saved in lowercase and deduplicated. Search includes tags, and the **Tag** filter selects an exact label. Revisions and published versions retain their tags; tags do not change prompt content or its hash. **Copy revision to draft** copies the template and tags.

1. Open an LLM or AI Decision node. Type `${` or press Ctrl+Space in its prompt editor for suggestions. Arrow keys select; Enter or Tab inserts; Escape closes.
2. Choose **Use typed template** to turn inline references into named inputs. Adjust names/types, map workflow data, and provide sample input JSON under **Preview and test**. Preview is free; **Run paid test** requires confirmation and executes one real LLM node under the saved workflow's team/project budget. It does not execute decision actions. Inspect the current test in the builder, or browse recent executions under **Management → Run history** and spend under **Cost analytics**.
3. Open **Management → Prompt library** to search prompts, filter draft/published status, sort by name or updated time, and create or edit full-size templates. Enter a name and prompt text, then save a draft or publish. New prompts receive an ID automatically from the name plus a generated suffix; you can customize it before the first save. Missing or invalid fields are highlighted before a request is sent, and server validation errors appear inside the editor. Library writes use revision-based conflict detection; reopen the prompt after a conflict. Unsaved editor changes require confirmation before leaving. Published versions cannot be overwritten. History records actor, timestamp, content and publish state atomically; **Copy revision to draft** copies template content into the current editor without rewriting history. Optional inputs require a default; select **Provide default value** to set one, including an empty string.
4. In an LLM or AI Decision node, open **Prompt library** and choose a published version to pin it to the node. **Manage prompt library** opens the dashboard through the normal unsaved-workflow guard. The server retrieves that exact team-local version and embeds its template/hash into the saved workflow and run snapshot. Publishing a new prompt does not change existing workflows, schedules, retries or replays. **Make local copy** removes the library link while preserving the template. Copying a historical revision creates editable local content; it never rewrites history.
5. Define workflow input types under the trigger's **Input types and validation** and custom node output types under **Advanced: output type**. Output schemas describe the entire node result, not just LLM `content`. They validate results before downstream execution; they do not change the provider's response format. Schema failures fail the run.

Suggestions show types, descriptions and optionality from schemas and known node contracts. Future nodes, unfinished parallel siblings and another decision action's local outputs are excluded. Conditional outputs after the join are optional. Unknown/dynamic outputs are not type guarantees; runtime validation still applies. This initial completion surface covers prompt text, instructions and typed prompt mappings, not JavaScript source or every inspector field. Arrays can be mapped as whole values; indexed expressions and schema unions/references are not expanded into completion paths.

Templates are limited to 50 variables, 16000 bytes per text field and 64 KiB total; rendered text is limited to 256 KiB. The team library allows 200 prompts and 10000 revisions. Templates, samples used for paid tests and resolved activity inputs may appear in authorized workflow histories: never paste credentials into prompts. Secrets remain external-API secrets, not prompt variables. AI instructions and typed inputs do not prevent model-level prompt injection; retain authorization, action allowlists and human approval policies.

The Studio Swagger interface documents `/api/v1/studio/prompts`, version/history/save/publish operations, preview/test and `/api/v1/studio/variables`. These are human-authenticated team APIs, not consumer endpoints. Viewers may read the library; writes, previews, tests and variable analysis require editor/admin access. Existing inline definitions retain their execution path. Deploy the new worker before authoring definitions with output schemas, because those definitions schedule the new output-validation activity.

Import [the typed prompt example](../examples/typed-prompt.json) to try the editor. Verification on 2026-10-06 covered template/mapping validation, immutable revisions, team isolation, PostgreSQL/MongoDB persistence, a real Temporal execution and history replay using a synthetic loopback provider, and the Angular editor against the Go API with test authentication. No paid provider compatibility test or deployment was performed. The browser regression is `scripts/prompt-editor-smoke.mjs`; its opt-in Go fixture is `TestPromptBrowserAPI` with `RUNLOOM_PROMPT_BROWSER=1`, and `RUNLOOM_PROMPT_API=http://127.0.0.1:17290` connects the browser script to that fixture.

### AI Decision

The `decision` node selects only designer-defined actions in `branches`. Each action has a unique `id`, a required `description` explaining when to choose it, and a nonempty `steps` path. The model cannot invent nodes, credentials, URLs or action arguments. Existing nodes, including pinned same-team subworkflows, implement the actions. See [the three-mode example](../examples/ai-decision-modes.json).

1. `decision.mode: "single"` chooses one action, once. `max_rounds` and `max_actions` are both 1.
2. `decision.mode: "multiple"` chooses a unique subset once. `execution` is `sequential` (model-selected order) or `parallel` (join all selected paths). `max_actions` bounds the subset.
3. `decision.mode: "agent"` chooses one action per round and feeds its results into the next decision. Execution is sequential. Defaults are 5 rounds and 10 total actions; maximums are 20 rounds and 50 actions. The final finish decision consumes a round. After the action allowance is exhausted, a remaining round may finish or hand off, but cannot select another action.

Each provider call requests strict JSON Schema output: `{"outcome":"actions","actions":["action-id"],"reason":"Brief explanation","hitl_required":false,"hitl_reason":""}`. The other outcomes are `finish` and `handoff`, both with an empty actions array. `handoff` requires `hitl_required: true` and a nonempty `hitl_reason`; actions and finish require false and an empty review reason. Contradictory responses fail rather than executing actions requiring review. The worker independently rejects invalid JSON, extra or duplicate fields, unknown or duplicate action IDs, refusals, truncated responses and violated limits. A compatible strict-schema model is required; there is no free-text fallback.

Every new decision round and the node's latest output expose `hitl_required` and `hitl_reason`. A completed handoff retains true even after the operator resumes, preserving the original assessment. Pre-change decision histories use the versioned legacy path; new provider responses cannot omit these fields.

`finish` completes this node and continues the workflow; it does not end the workflow. `handoff` pauses with the model's reason until an authorized operator resumes or cancels. Resuming continues after this node without re-running the model or approving an action. Exhausted round limits and invalid decisions fail the run. Failed actions fail fast and cancel active siblings; completed external effects are not rolled back. Put sensitive actions behind explicit Approval and Condition nodes.

The model settings match `llm`, with one provider attempt by default and at most three attempts per round. `timeout_seconds` limits each model attempt, not the whole decision node or human waiting. Configure individual wait, callback, approval and child-workflow timeouts as needed. Budget pauses preserve completed paid responses and require the existing budget recheck/resume process.

There are 1–20 actions per node. Nested AI Decision nodes and End nodes inside action paths are rejected; a child workflow can contain its own bounded decision. Each action invocation receives fresh runtime step, signal and child identifiers. Integrations must use the active identifiers from run status, not template IDs, so stale replies cannot unlock a later round.

Action paths can reference workflow input, earlier outer steps and earlier nodes in that same path, but not sibling actions. `${decision.round}`, `${decision.reason}` and `${decision.previous}` expose round context. Repeated decisions receive the previous round records automatically; context exceeding 256 KiB fails rather than silently dropping history.

Node output includes `mode`, `outcome`, `actions_executed` (successful actions), `selected_actions`, `reason`, `results` and `rounds`. Each round records its selected actions, reason, per-action step outputs, model and usage. `results` is the latest round's output map, empty for finish/handoff; read `rounds` to retain earlier results. Recorded Temporal decisions replay without repeating provider calls. Model reasons and action results may contain sensitive information and remain subject to existing run-history access controls.

### Wait

A wait uses a Temporal durable timer. `seconds` must be from 1 through 31,536,000. It always resumes automatically when the timer ends. It does not wait for a person or external system.

### Callback

A callback pauses at a durable gate until an external system sends the `callback` signal for the exact run and step identifier. `callback_key` names the expected business event for integrations and audit screens. The event contains JSON `payload` plus an `actor`; both become the step output. The API replaces any supplied actor with the verified user subject and requires a team editor/admin token plus `X-Runloom-Team`. An optional timeout can either fail the run or continue with a timed-out output. Integration-level idempotency and replay protection remain operator responsibilities.

### Independent Temporal signal-handler paths

Top-level `signal_handlers` define independent event entry paths within the same running execution. Handler actions use `${signal.payload.field}`, `${signal.actor}` and `${signal.name}`. They run serially while the main path continues; End returns from the handler, and main completion drains received work before closing. Names are unique across handlers and inline waits. See [signal-handler authoring, delivery, lifecycle and rollout](signal-handlers.md).

### Named signal wait

A signal node pauses at a durable gate identified by `signal_name`. The integration endpoint accepts JSON `payload` plus an optional `actor`. A matching event can arrive before the workflow reaches the node and is consumed when that node becomes active. Signal names must be unique across a definition. An optional timeout can fail the run or continue with a timed-out output.

### Approval

An approval exposes a durable gate keyed by the step identifier. A decision contains `approved`, optional `comment`, and optional `actor`. On timeout, `on_timeout: reject` creates a rejected decision; `on_timeout: fail` fails the workflow.

### Pause

A pause is an operator checkpoint. It remains stopped until a `resume` signal names the exact run and step identifier. Resume data and the actor become the step output. An optional timeout can fail the run or resume it automatically. A pause is not cancellation: cancellation permanently closes the run and cannot be resumed.

### Condition

A condition resolves `if.left` and `if.right`, applies one of `equals`, `not_equals`, `contains`, `exists`, `greater_than`, `greater_than_or_equal`, `less_than`, or `less_than_or_equal`, then executes either `then` or `else`.

### Parallel

A parallel step runs every branch concurrently and waits for all branches. The default failure mode is fail-fast at the workflow level. Step identifiers remain globally unique across branches.

### Data building nodes

`transform`, `collection`, `merge`, `foreach`, `fail` and `integration` are additive schema 1.0 node types. Deploy the updated workflow API and worker before publishing them. Older workers do not implement these types. The [data-building sample](../examples/data-building.json) uses only local data operations, without credentials or external side effects.

Import the sample and choose **Test workflow** with this input; it returns one prepared customer and `count: 1`:

```json
{"customers":[{"name":"Ada","active":true},{"name":"Inactive","active":false}]}
```

1. `transform` uses `data.operation: "map"`, `data.source` and a `fields` array of `{name,value,type,use_default,default}` mappings. `${item.field}` refers to the source object. `keep_source` retains original fields; mapped fields overwrite matching names. Conversion types are `string`, `number`, `integer` and `boolean`; `object` and `array` assert the existing type. Omit the type to preserve it. Defaults apply only to missing or null values, not failed conversions.
2. `collection` supports `filter`, `sort`, `limit`, `unique`, `count`, `sum`, `split` and `compare`. Filter uses a condition in `data.predicate` with `${item.field}` and `${index}` references. `key` is a dotted field path. Sort is stable; `descending` reverses ordering. Limit zero returns an empty array. Count and sum return objects (`{count}` or `{sum,count}`). Split flattens arrays at `key`. Compare returns source records absent from `other`; unique and compare can omit `key` to compare whole JSON values.
3. `merge` supports `append` and `join` on `source` and `other` arrays. Join requires `key` and `other_key` and returns `{left,right}` pairs for matching keys. It is an inner join; duplicate keys produce multiple results. It does not execute or synchronize branches—use Parallel for that.
4. `foreach` uses `foreach.items`, `foreach.output`, `foreach.concurrency` (1–10) and exactly one nonempty branch named `body`. Body steps can reference `${loop.item}` and `${loop.index}`. Results retain input order even when work completes out of order. Concurrency runs in bounded batches. Body outputs do not leak into outer step variables. Callback, signal and child identifiers are scoped per iteration; integrations must use the active runtime identifiers. A failed item stops the loop and cancels active siblings without undoing completed external effects.
5. `fail` ends execution as failed with a required `message`. It must be last in its path. It differs from End, which completes successfully; neither implements recovery or compensation.

Data arrays are limited to 1,000 items and source/results to 256 KiB. For Each allows at most 100 items and 1,000 conservatively counted body-node executions. Nested loops and End inside loop bodies are rejected; use child workflows for separately bounded work. Missing fields, incompatible types, non-finite numbers and invalid conversions fail explicitly. Drafts may retain invalid node definitions; publication and test execution require backend validation.

### Managed integration operations

The `integration` node holds `{operation,secret,parameters}`. `secret` is a team-vault name containing a provider access token, never the token itself. Parameters accept workflow expressions. Operations and parameters come from the workflow API's `/api/v1/studio/catalog` response and are independently allowlisted by the worker.

| Operation | Capability |
| --- | --- |
| `sheets-read`, `sheets-append` | Read a Google Sheets range or append raw rows |
| `drive-list` | List Google Drive file metadata |
| `gmail-list` | List Gmail message identifiers |
| `calendar-list` | List Google Calendar events |
| `slack-send` | Send a Slack channel message |
| `teams-send` | Send a Microsoft Teams channel message |
| `outlook-send` | Send an Outlook email |
| `notion-search` | Search Notion |
| `hubspot-contacts` | List HubSpot contacts |

Each operation uses a fixed public HTTPS endpoint, existing SSRF/TLS checks, vault authorization and response-redaction controls. Output contains `status_code` and `body`. Provider access scopes and resource permissions must be configured by the operator; Microsoft user actions require suitable delegated tokens. The node does not create OAuth connections or refresh tokens. List operations fetch one page; follow provider cursors explicitly. These are action nodes, not inbound triggers.

Requests have a 60-second start-to-close timeout and one attempt. Mutating actions also run during workflow tests. A timeout does not prove the provider did not apply the action, and rerunning a workflow may duplicate an external effect. Do not place unapproved side effects before a human approval gate. No real-provider credentials are needed for the deterministic contract tests; those tests do not certify a live provider account.

### Typed LLM tasks

An existing `llm` node can add `llm_task.kind` of `extract`, `classify`, `summarize` or `sentiment`, while retaining its prompt, model, library reference and usage tracking. General LLM nodes remain unchanged.

1. Extraction defines 1–50 unique typed `fields` with `name`, `type` (`string`, `number`, `integer` or `boolean`) and optional `description`. Every requested field is returned; unavailable values are null and require human review.
2. Classification supplies 2–50 unique `labels`. The output contains `label` and `reason`. A null label requires human review.
3. Summarization returns `summary` and `key_points`.
4. Sentiment returns `sentiment` (`positive`, `neutral`, `negative`, `mixed` or `unknown`) and `reason`. Unknown requires human review.

The response envelope contains `content`, typed `data`, `hitl_required` and `hitl_reason`; normal model/usage metadata is retained. A strict-schema-compatible model is required. Backend parsing rejects invalid schema, extra fields and contradictory missing-value/review responses. Typed output does not prove factual accuracy or eliminate prompt injection. Unlike AI Decision handoff, these task presets do not pause automatically: use Condition and Approval before downstream sensitive actions.

### Delivery boundaries

This increment supplies bounded data-building nodes, token-based integration actions and typed LLM presets. It does not yet provide forms, managed business records, file-storage nodes, error-trigger recovery workflows, full provider OAuth/inbound events, workflow-facing chat, retrieval/vector pipelines, MCP tools or a model-quality evaluation dashboard.

Run `make test-building` for contract/runtime and editor tests. `make test-authoring` provisions and removes disposable PostgreSQL and MongoDB containers and checks draft/publish persistence for the new definitions. With a disposable Temporal server, set `RUNLOOM_TEST_TEMPORAL_ADDRESS` and run `make test-building-live` for execution and history replay. With the development host and workflows remote running, `make test-building-browser` checks real Angular controls against synthetic APIs; `RUNLOOM_UI_URL`, `RUNLOOM_PLAYWRIGHT_MODULE` and `RUNLOOM_CHROME` select the local host, installed Playwright module and browser binary.

## Limits

Each implementation rejects definitions larger than 1 MiB, more than 500 total steps, nesting deeper than 10 levels, duplicate or invalid identifiers, duplicate named signals, and unsupported schema versions or operators. Callback keys and named signals use the same restricted naming form as registered activities. Callback, signal, approval, and pause payloads are JSON only; definitions never contain executable callback code or model credentials.
