# Independent Temporal signal-handler paths

Source behavior updated: 2026-11-10. Audience: workflow builders, integration developers and self-hosted operators. Deployment must be verified separately.

## 1. Behavior

A signal handler receives events on an already-running workflow execution and executes a separate action path while the main path continues. It does not start a new workflow. The existing inline **Signal** node remains a wait inside a path; it is a different feature.

Handler invocations are serialized per execution. Messages in each signal channel retain their order. When different native channels are already buffered together, collection follows the declared handler order; there is no cross-channel total-order guarantee. An **End** in a handler returns from that invocation, while **Fail** or an action failure fails the workflow and cancels the main wait. The handler cannot directly mutate main-path input or outputs; use its event payload, workflow input and its own preceding action outputs. External database/API effects are not rolled back on cancellation or failure.

The main path must remain running to receive future events. For a long-lived listener, design a suitable Wait, Pause or other durable main-path gate. When the main path finishes, the workflow drains received handler work and closes when the queues are empty. It can still receive events while draining because the execution remains open. A completed workflow does not remain an event listener. There is no automatic Continue-as-New policy in this release.

Runtime evidence: [handler processor](../server/workflows/internal/engine/signal_handlers.go), [interpreter](../server/workflows/internal/engine/workflow.go).

## 2. Build a handler in Studio

1. Open a saved workflow and select **Edit workflow**.
2. Expand **Signal handler paths**, enter a unique event name such as `customer.updated`, and select **Add signal handler**. Its independent entry point appears below the main flow.
3. Use the connection buttons on that path to insert actions. The default **Return from handler** ends only this invocation. Select a handler entry to focus its event configuration; select an action to edit its usual parameters.
4. Reference `${signal.payload.customer_id}`, `${signal.actor}`, `${signal.name}` and optional `${signal.event_id}` in action mappings. The variable picker includes handler payload fields when `input_schema` declares them.
5. Use workflow JSON to configure the handler's optional `input_schema`. Invalid draft content can be saved, but validation blocks publication and testing until corrected.
6. Save and validate. Publishing is a separate action. Existing runs keep their original immutable snapshot; editing or publishing a handler does not add it to a run already in progress.
7. In **Test workflow**, the run panel lists independent handlers with received, queued and completed counts. Supply **Event payload (JSON)** and select **Send handler event**. Acceptance is not completion. Testing executes real configured activities; use synthetic data and sandbox connections.

## 3. Workflow contract

`signal_handlers` is optional and additive to schema version `1.0`. A definition supports at most 20 handlers. Handler IDs, action IDs and existing main-path node IDs share one namespace. Handler signal names are unique across all handlers and inline signal waits. `signal`, `approval`, `callback` and `resume` are reserved transport names. Handler entry points count toward the existing 500-node definition limit; nesting is still bounded to 10 levels.

Example using only a timer and the local echo activity:

```json
{
  "schema_version": "1.0",
  "id": "customer-events",
  "name": "Customer event listener",
  "kind": "main",
  "trigger": {"type": "manual"},
  "input_schema": {},
  "steps": [
    {"id": "main-wait", "type": "wait", "seconds": 60},
    {"id": "main-end", "type": "end"}
  ],
  "signal_handlers": [
    {
      "id": "on-customer-update",
      "name": "Customer updated",
      "signal_name": "customer.updated",
      "input_schema": {
        "type": "object",
        "required": ["customer_id"],
        "properties": {"customer_id": {"type": "string"}}
      },
      "steps": [
        {
          "id": "record-update",
          "type": "activity",
          "activity": "runloom.echo",
          "with": {"customer_id": "${signal.payload.customer_id}"}
        },
        {"id": "handler-return", "type": "end", "with": {"handled": true}}
      ]
    }
  ]
}
```

The authoritative shape is in [workflow.schema.json](../spec/workflow.schema.json); [backend normalization](../server/workflows/internal/definition/validation.go) additionally enforces reserved names, global identifiers, schema restrictions and runnable node configuration. Child dependencies and prompt references inside handlers use the same team-scoped resolution and version pinning as the main path.

## 4. Deliver an event

An authorized Studio human editor/admin can send an event using the existing endpoint:

```http
POST /api/v1/studio/runs/<workflow-id>/signals/customer.updated
Authorization: Bearer <studio-human-token>
X-Runloom-Team: <team-id>
Content-Type: application/json

{
  "event_id": "5f67b371-e86e-425d-bb0a-e05047c2581e",
  "payload": {"customer_id": "demo-123"}
}
```

Use the execution's workflow ID, not the definition ID or Temporal Run ID. HTTP 200 means accepted, not processed. The server supplies the authenticated actor. With `event_id`, live handler state permits early validation: invalid payloads return 422, a closed handler returns 409, and observed queue/event limits return 429. If that live query is unavailable, execution-side validation still applies. These routes remain unavailable to consumer service accounts; do not embed a client secret or human token in an untrusted frontend. Cross-team run authorization is unchanged.

A trusted Temporal client can instead send the actual declared signal name to the execution's namespace. It must use the deployment's configured payload codec, encryption keyring and transport security, just like the Runloom worker; a default plaintext SDK client cannot communicate with an encrypted deployment:

```go
err := temporalClient.SignalWorkflow(ctx, workflowID, "", "customer.updated", map[string]any{
    "event_id": "5f67b371-e86e-425d-bb0a-e05047c2581e",
    "payload": map[string]any{"customer_id": "demo-123"},
})
```

The native envelope supports only `payload` and optional `event_id`. `payload` is required and may be any JSON value allowed by the handler schema. Native messages are attributed as `temporal-client`, not as an authenticated Studio user. Direct Temporal access requires separately authorized platform credentials and the correct organization namespace; it does not bypass Runloom's HTTP API through a consumer credential. Native deliveries live in Temporal history and runtime state; they do not pass through the HTTP audit endpoint. See [Temporal's Go message-passing guide](https://docs.temporal.io/develop/go/message-passing).

## 5. Repeats, limits and recovery

1. Without `event_id`, every event is a separate invocation. With a UUID `event_id`, repeating the same handler, sender and payload is deduplicated for this execution. Reusing that key with changed data or sender fails the workflow rather than silently treating it as the same event. Different runs have separate ledgers. This does not make external activities exactly-once: use the event ID in downstream idempotency controls where needed.
2. The event payload limit is 64 KiB of canonical JSON, with at most 32 data nesting levels. There are at most 100 queued handler events and 1,000 accepted handler invocations per execution, across all handlers. Duplicates do not increment invocation counts. Runtime overflow fails the run; there is no silent queue eviction. These bounds do not guarantee that a high-volume execution will remain below Temporal history limits; use short, bounded runs.
3. Invalid native envelopes, invalid payloads and handler action failures fail the run. Main-flow failure or cancellation cancels handler work. Already completed external effects remain completed. Inspect handler status and activity history before retrying an operation with side effects.
4. Each invocation gets unique runtime step, callback, approval, pause, signal and child identifiers. Use active IDs from run status when replying to gates inside a handler; a template ID or a prior invocation's ID cannot target the next invocation.
5. Handler payloads and action outputs may appear in run state, audit records and Temporal history under the existing execution-storage rules. Do not send credentials as event data. Use configured secret and connection references in action definitions.

## 6. Upgrade and verification

Upgrade every worker before enabling the new API/Studio. Handler-enabled root, child and test snapshots use workflow types ending in `WithSignalHandlers`; this includes roots whose pinned dependencies contain handlers. An old worker cannot silently deserialize away handler paths and run only the main sequence. Old workers may fail tasks for an unknown new type until upgraded. Existing histories without handler snapshots retain their original workflow types and execution paths. Rolling back workers while new handler executions remain active is unsupported; drain or retain compatible workers first.

No database migration or credential rotation is required: definitions and snapshots retain the new optional JSON field through the existing stores. The API still uses current human authorization for event delivery. Lumi receives the updated contract and can build/edit independent paths, subject to the same scope, validation and draft-save checks.

Verification evidence is maintained with [runtime tests](../server/workflows/internal/engine/signal_handlers_test.go), [contract tests](../server/workflows/internal/definition/signal_handlers_test.go), [delivery tests](../server/workflows/internal/api/signal_handlers_test.go), and [canvas tests](../studio/projects/workflows/tests/canvas-layout.test.mjs). Passing Temporal SDK tests demonstrate the simulated cases; live verification and deployment must be reported separately.

Verification on 2026-11-10 used the working tree in local `kind-runloom-local`, namespace `runloom`. Runtime, definition, API and model Go tests, Assistant chat tests, 71 Studio tests, the Studio production build and generated OpenAPI checks passed. Disposable synthetic draft tests verified HTTP delivery and deduplication against Temporal, native named-signal delivery using the deployment codec, handler completion and real encrypted-history replay. No workflow was published. The static `workflowcheck` command remains blocked by its finding in the unchanged `definition.ResolveDatabase` JSON conversion under Go 1.27.3; successful history replay does not establish replay coverage for every node combination.
