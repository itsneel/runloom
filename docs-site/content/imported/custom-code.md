# Custom Code: JavaScript

Add **Custom Code** in Studio's node picker. Map workflow data into **Mapped workflow input**, write JavaScript, then use **Test JavaScript** with a sample JSON object. Tests run server-side using the same isolated runner as workflow execution, without saving or publishing. Edit locks and unsaved-change warnings apply to source and mappings. Sample input is temporary; results disappear when the tested source or sample changes.

## Contract

```json
{
  "id": "transform",
  "type": "code",
  "code": {
    "runtime": "quickjs-ng-0.11.0-v1",
    "source": "return { greeting: `Hello ${input.name}!` };"
  },
  "with": { "name": "${input.customer.name}" }
}
```

The source is a synchronous JavaScript function body. It receives an `input` object and must explicitly return JSON: an object, array, string, finite number, boolean or null. Undefined, functions, BigInt, promises, circular values and excessive nesting fail the node. TypeScript syntax is not transpiled and fails with a syntax error. There is no Node.js environment, npm dependency installation, browser DOM or fetch API.

Only `with` is resolved by Runloom. Source is never interpolated, so JavaScript template strings remain intact. A following node can reference `${steps.transform.output.greeting}`. Source and the pinned runtime identifier are included in workflow drafts, commit history and immutable published versions. Do not change an existing runtime identifier's contract when upgrading engines: retain it or provide an explicit version migration.

The runnable [order example](../examples/javascript-transform.json) accepts [order input](../examples/inputs/order.json) and returns `{"order":{"total":40,"itemCount":2}}`. Import and save it as a draft before starting a Studio workflow test; publishing is not required.

## Execution and safety

1. A normal Temporal Activity invokes `runloom-code` in a new subprocess. User JavaScript never executes in workflow replay, the API process or the worker process. Recorded Activity results are used during replay; code is not rerun by replay.
2. The runner embeds pinned QuickJS-NG 0.11.0 WebAssembly in wazero. WASI has no filesystem preopens, environment, network or secret access. No host credentials are inherited by the subprocess. The capability boundary is WebAssembly, not JavaScript global removal or Node's `vm`.
3. Limits are 32 KiB UTF-8 source, 256 KiB JSON input/output, 32 MiB JavaScript heap, 64 MiB WASM linear memory and a 5-second runner deadline. The parent also kills the subprocess after 6 seconds, including startup. These are not total operating-system RSS limits: budget additional Go/WASM host memory in container resources.
4. Each API or worker process permits two runner processes globally and one per team, with no unbounded waiting queue. Preview returns HTTP 429 on saturation. Workflow Activities retry capacity/unavailable errors at most five times within a 60-second schedule-to-close deadline; script and data-contract errors are non-retryable. Activity execution can still be repeated after worker failure; code must remain side-effect-free. Limits are per process, not cluster-wide quotas. For larger multi-tenant deployments, add Temporal Task Queue Fairness and cluster-wide admission control.
5. Use External API nodes and team secrets for external calls. Do not embed credentials in source or mapped input: workflow definitions, run history and Activity payloads can contain them. Preview audit events contain actor, runtime and outcome only, not source, test input, output or error details.
6. Canceling a workflow closes its Activity wait; an in-flight runner is bounded by its hard deadline. Preview disconnection cancels its runner. JavaScript cannot implement durable waits, callbacks, signals or pauses; use their dedicated workflow nodes.

The runtime is an additional security boundary, not a guarantee against engine vulnerabilities. Keep pinned runtime dependencies patched, use non-root read-only containers, apply memory/CPU limits, and restrict workflow editing to trusted team members. Arbitrary public anonymous code execution is not supported.

## Studio preview API

`POST /api/v1/studio/code/test` requires interactive Studio authentication and an editor/admin role in `X-Runloom-Team`. Consumer service-account tokens are rejected. Request: `{"code":{"runtime":"quickjs-ng-0.11.0-v1","source":"return input;"},"input":{"example":true}}`. Response: `{"output":{"example":true}}`.

Errors use the Studio `{"detail":"..."}` envelope: 401/403 for authentication/authorization, 422 for invalid code/input/output or execution limits, 429 for capacity, and 503 when the runner binary is unavailable. This is a code preview, not a persisted workflow run; use normal workflow testing for run history and orchestration testing.

## Build and deploy

`make build-go` builds the API, worker, healthcheck and `bin/runloom-code`. `make run-go-api` and `make run-go-worker` build and configure the runner automatically. If using `go run` directly, first run `make build-code`, then set `RUNLOOM_CODE_RUNNER` to the absolute path of that binary. Built API/worker binaries find the runner alongside themselves by default.

The shared server Dockerfile ships `/runloom-code` beside both server binaries. Existing Compose and Helm API/worker deployments therefore need no extra service, exposed port, Node installation, credentials or secret mount. Rebuild backend and Studio images, then roll out both API and worker before publishing code-node workflows. Old workers cannot execute the new node. Existing workflow branches and command sequences are unchanged; future edits to existing Temporal command paths still require replay/versioning review.

Run `make test-code` for the runner, limits, Activity mapping, validation and API tests. The Studio browser regression is `scripts/code-node-smoke.mjs` (development server); `scripts/code-node-live-smoke.mjs` verifies a local authenticated preview and a published workflow through Temporal.
