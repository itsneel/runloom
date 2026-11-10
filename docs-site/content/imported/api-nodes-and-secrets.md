# External API calls, workflow secrets and completion

## Builder

1. Add an **External API** node. Choose GET, POST, PUT, PATCH, DELETE, HEAD or OPTIONS, enter a public HTTPS URL, and set a timeout (1–300 seconds).
2. For authentication, ask a team administrator to open **Management → Workflow secrets**. Save a name and value; there is no allowed-origin setting. Values are write-only; saving the same name replaces the value, and deleting it prevents future resolutions. Every editor in the team can use these credentials through a workflow with any supported public HTTPS destination, so only grant editor access to trusted authors.
3. Enter the secret's name in the node's **Bearer secret name** field. For an API-key header or Basic authentication, use the request configuration shown below. For Basic, store the provider-required base64 username/password string as the secret. Values never belong in workflow JSON, URLs, ordinary headers, input or request bodies.
4. Use `${input.order_id}` or `${steps.previous.output.body.id}` in URL paths/query values and the JSON body. The host is fixed in the definition; credentials are resolved by team and secret name. URL substitutions are literal, not automatically URL-encoded; supply encoded values where needed. Plain headers are literal strings in this version.
5. Read `${steps.call-api.output.status_code}` and `${steps.call-api.output.body}` in later steps. Only 2xx responses succeed. Non-JSON bodies, oversized responses and redirects fail. HEAD and empty responses produce a null body. Headers and error bodies are not returned.

```json
{
  "id": "call-api",
  "type": "http",
  "http": {
    "url": "https://api.example.com/orders",
    "method": "POST",
    "headers": { "Accept": "application/json" },
    "secret_headers": { "X-API-Key": { "secret": "billing-api" } },
    "body": { "order_id": "${input.order_id}" }
  },
  "timeout_seconds": 30,
  "retry": { "maximum_attempts": 1 }
}
```

The activity defaults to one attempt, including an empty retry configuration. Opt into retries only when the provider supports an appropriate idempotency contract. Temporal durability is not exactly-once external side effects; even a canceled/timed-out call may already have changed the provider. Connection errors, 429 and 5xx can retry when enabled; other HTTP failures are non-retryable. Use separate nodes or a registered integration activity for OAuth token acquisition, multipart uploads, streaming, dynamic header authentication or other advanced protocols.

## Automatic completion and End

Without an End node, a workflow completes successfully after its last step, once all waits, subworkflows and parallel branches have finished. There is no implicit time limit that skips pending waits; configure their existing timeout behavior when needed.

An **End workflow** node explicitly completes the current workflow successfully, with its `with` object as the final result (empty object if omitted), instead of evaluating the definition-level output. Its completion message appears in step history. Inside a decision branch, End also skips subsequent outer steps. Ending a child returns to the waiting parent; it does not end the parent. End must be last in its own sequence. End anywhere inside a parallel branch is rejected: put it after the parallel join. This avoids ambiguous cancellation of in-flight sibling calls.

```json
{ "id": "done", "type": "end", "message": "Workflow will end now", "with": { "receipt": "${steps.call-api.output.body.id}" } }
```

See [the sample definition](../examples/external-api.json). It needs an administrator-provisioned `billing-api` secret and a real trusted destination replacing the example host before execution.

## Operator setup

Secrets are encrypted with AES-256-GCM in the existing access directory (PostgreSQL, MongoDB or single-API filesystem mode). Authenticated encryption binds each value to its team and name. API and worker must receive the same persistent 32-byte binary key through `RUNLOOM_SECRETS_KEY_FILE`. Local Helm installs and `make local-up` / `make fresh-up` provision and reuse a key automatically. Other native/Compose deployments require explicit key configuration. Without it, unauthenticated external calls remain available but saving or using credentials fails closed.

For automatic local setup, run `make local-up` (Compose) or `make k8s-up` (Kubernetes). To create/reuse only the local file without starting services, run `make setup-workflow-key` without a path. See [local key lifecycle and overrides](local-development.md#commands).

For an explicitly managed key file, create a private parent directory outside the repository, then run:

```sh
export RUNLOOM_SECRETS_KEY_PATH=/absolute/private/workflow-encryption-key
make setup-workflow-key
make docker-config-secrets
make docker-up-secrets
```

The key-generation command creates a new `0600` file and never prints the value or overwrites an existing file. Back it up separately from the database. Do not regenerate it on restarts or change it without a re-encryption migration; existing ciphertext will become unreadable. Automated encryption-key rotation/re-encryption is not implemented. Credential value rotation is supported by saving a replacement under the same name; future activity attempts read the latest value, not a workflow-version-pinned value.

Compose mounts the key only into API and worker. Ensure container UID 65532 can read it without making it world-readable. For native development, set `RUNLOOM_SECRETS_KEY_FILE` to this file in both processes. To enable both consumer provisioning and workflow secrets, preserve both overlays:

```sh
docker compose --project-directory . -f compose/compose.yaml -f compose/compose.consumer.yaml -f compose/compose.secrets.yaml up --build --detach --wait
```

The above also requires the provisioner file path from [consumer setup](consumer-api.md). For hosted Helm deployments, provision an existing Kubernetes Secret with the raw 32-byte key through your secret-management process, then set `workflowSecrets.existingSecret` and optionally `workflowSecrets.secretKey` (default `encryption-key`). The chart mounts it read-only into API and worker, never the UI.

Alternatively, configure `workflowSecrets.externalSecret` to have External Secrets Operator populate the mounted Kubernetes Secret from a preconfigured provider store. See [chart configuration and migration precautions](../charts/runloom/README.md#external-secrets-operator). This is optional and requires ESO installed separately; the application itself never fetches provider secrets.

Docker Compose uses `compose/compose.secrets.yaml` with a host file supplied through `RUNLOOM_SECRETS_KEY_PATH`. A host-side secret-manager agent may provision that file as well; provide the original raw 32-byte key, not base64 text. Compose does not run ESO or retrieve cloud secrets. Keep the same file path and contents across restarts. Do not add the key value to `.env`, Docker build arguments, images or workflow JSON. `make docker-config-secrets` validates the overlay without starting containers; `make docker-up-secrets` starts it. If using a custom Compose project/environment, pass the same files, project name and environment options with the secrets overlay to avoid starting a second stack.

Local Helm development releases create `<release-fullname>-workflow-key` on first install, reuse it via cluster lookup on upgrades, and retain it on Helm uninstall. Workflows and worker share its read-only mount and require a working vault in their readiness checks. If a connected upgrade finds an existing Workflows Deployment but no original key Secret, it fails rather than silently replacing the key. An explicit `workflowSecrets.existingSecret` always takes precedence. Back up the key separately; deleting the namespace/cluster loses it. Do not apply offline-rendered development manifests over an existing key: offline rendering cannot look up the installed value. Hosted deployments never auto-generate a key.

Existing origin-bound records remain decryptable using their legacy encryption metadata, but their origin no longer restricts usage. Saving a replacement removes the legacy metadata. Deploy the updated API and workers together before using origin-free secrets; older workers cannot resolve new records.

## Security boundaries

1. Human team admins may create/replace/delete secrets; team members can list names and update timestamps. No endpoint returns plaintext or ciphertext. Consumer credentials cannot access these management endpoints. Save/delete audits contain identity and secret name, never value.
2. HTTP and secret I/O happen only inside `runloom.internal.http`. Definitions cannot invoke internal activities directly. Temporal activity inputs contain secret references, not stored values. Published definitions remain immutable; existing node paths do not add new replay commands.
3. Only public HTTPS destinations on port 443 are supported. Dial-time DNS resolution rejects private, loopback, link-local and special-use IPs, then connects directly to the validated address. Redirects and environment proxy discovery are disabled. Keep worker egress firewall restrictions as defense in depth, especially with custom DNS/NAT arrangements. These controls follow the [OWASP SSRF prevention guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).
4. Requests and responses are bounded to 256 KiB. Returned JSON redacts known credential strings, common encodings, and sensitive key names before reaching Temporal history. This is not a universal data-loss-prevention system: a trusted provider could transform/encode values in other ways, and responses can contain business or personal data. Only use trusted destinations and review returned data. A secret is not restricted to a destination; all team editors must be trusted with its use. Do not use credential/token issuance endpoints as ordinary workflow data sources.
5. The initial encrypted secret catalog shares the bounded access directory and permits up to 500 secrets overall. Filesystem deployments support one writer API process; hosted replicas should use PostgreSQL or MongoDB. Administrative database/worker access and the encryption key remain privileged. TLS is required for hosted Studio/API traffic; proxies/APM must not record secret request bodies or Authorization headers.

## API and verification

`GET /api/v1/studio/orgs/{org}/teams/{team}/secrets` returns `{configured, items:[{name, updated_at}]}`. `PUT .../secrets/{name}` accepts `{value}` and returns only `{saved:true}`. `DELETE .../secrets/{name}` removes the entry. Names start with a lowercase letter and contain up to 64 lowercase letters, digits or hyphens. Values are 1–8192 bytes, without CR, LF or NUL (they are injected into HTTP headers). Existing Studio bearer authentication is required. Responses are `Cache-Control: no-store`.

Studio highlights invalid fields and shows an inline explanation as you edit. Spaces, including leading/trailing spaces, are not allowed in secret names; use a hyphen instead. Spaces in secret values are allowed and preserved exactly. Fixing an invalid field clears its error; a successful save clears the value and resets validation feedback. Invalid forms never submit a save request.

Run `make test-go-race check-go helm-lint build-ui`. `make test-ui-api-nodes` uses synthetic credentials and mocked API responses against the development server; it requires Playwright and the same `RUNLOOM_UI_URL`, `RUNLOOM_PLAYWRIGHT_MODULE` and `RUNLOOM_CHROME` options as the other [UI checks](../studio/README.md). These checks do not call real third-party services or provision real secrets.

For browser save/replace/delete checks against the real Go vault, start the three Studio development apps, then run this opt-in fixture from `server/` in another terminal:

```sh
RUNLOOM_PROMPT_BROWSER=1 RUNLOOM_SECRETS_BROWSER=1 RUNLOOM_DASHBOARD_BROWSER=1 go test ./internal/api -run '^TestPromptBrowserAPI$' -count=1 -v
```

Once it reports ready on `127.0.0.1:17290`, run `make test-ui-secrets` at the repository root with the browser options above. This uses temporary file storage, a synthetic encryption key and test authentication. It verifies the value-only contract, save-error recovery without echoing credentials, replacement/deletion and team permissions. It does not use the local cluster or execute Temporal workflows. The fixture exits after the browser finishes (or times out after three minutes).
