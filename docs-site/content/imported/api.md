# Runloom API documentation

Interactive Swagger UI is served by the Go API at `/api/docs/studio/` and `/api/docs/consumer/`. Open these paths on your running Studio/API origin (for example `http://localhost:4200/api/docs/studio/` after upgrading a local installation). Each page has its own specification and Authorize control. JSON downloads are `/api/v1/studio/openapi.json` and `/api/v1/openapi.json` respectively.

The docs and specs are publicly readable; execution endpoints retain all authentication, role, scope, grant and ownership checks. Paste only an access token into Authorize. Studio requires the human token and a team header on scoped requests; Consumer requires an execution service-account token. Tokens are not persisted, and switching docs pages does not carry authorization across interfaces. Try it out sends real requests, not simulations. Static assets are embedded locally, with no CDN or online validator.

Backend builds automatically embed committed specifications and assets; Docker/Helm need no extra service or asset download. To maintain the docs, run `make setup-api-docs`, `make build-api-docs`, then `make test-api-docs`. The generator is `server/api-docs/studio-spec.mjs`, with authoring metadata in `authoring-spec.mjs` and `catalog-deletion-spec.mjs`; it imports workflow/schedule JSON schemas and writes the downloadable Studio specification. Change generator metadata or source schemas first, then regenerate: do not hand-edit `server/workflows/internal/api/studio.openapi.json`, because regeneration replaces it. Node regression tests protect deletion operations, the integration catalog and current node schemas; Go route coverage tests detect missing/extra Studio operations. Browser tests additionally run when `RUNLOOM_PLAYWRIGHT_MODULE` is set to an installed Playwright module, optionally with `RUNLOOM_CHROME` naming the browser executable. Deploying a new API build is required for existing installations to serve these pages.

Runloom has separate API contracts for the Angular Studio applications and external backend consumers. Choose the guide for your caller; credentials and permissions cannot be substituted between interfaces.

Verified against repository code on 2026-10-02. Only `v1` is implemented. These documents describe the code, not confirmation that a running installation has been upgraded.

| Guide | Audience and base path | Access model |
| --- | --- | --- |
| Studio Swagger `/api/docs/studio/` and [OpenAPI JSON](../server/workflows/internal/api/studio.openapi.json) | Angular host, workflow builder and management; `/api/v1/studio` | Verified human login and organization/team roles |
| Consumer Swagger `/api/docs/consumer/` and [integration guide](consumer-api.md) | Backend integrations; `/api/v1` | Service-account token, explicit workflow/version grants, operation scopes and owned root runs |
| [Studio workflow execution details](workflow-api.md) | Start, status, resume and cancel examples for human callers | Same Studio permissions; not a consumer interface |
| [Consumer OpenAPI JSON](../server/workflows/internal/api/consumer.openapi.json) | Import into an API client or generate a client | Served publicly at `GET /api/v1/openapi.json`; execution requests still require authentication |

## Supported workflow operations

A workflow definition is the reusable design. A run is one execution of a published version or Studio draft snapshot. Status, resume and cancellation address a run ID, not a definition ID.

| Operation | Studio | Consumer |
| --- | --- | --- |
| List accessible workflows | Team definitions, including drafts | Explicitly granted published main versions |
| Start a published main workflow | Editor/admin | `runs:start` and exact workflow/version grant |
| List runs and get status | Authorized team's runs | `runs:read`, own root runs and retained version grant |
| Stop permanently | Request cancellation | Request cancellation with `runs:cancel` |
| Stop temporarily or pause on demand | No general run endpoint; design a Pause node | Not implemented |
| Resume an existing pause | Editor/admin, addressed to the paused run/step | Not implemented; do not use a Studio token as a workaround |
| Cancel a run | Editor/admin | `runs:cancel`, own root run and retained version grant |
| Force terminate | No public Runloom endpoint | No endpoint |
| Test unpublished main/child draft | Separate Studio test action | Not permitted |

Cancellation is asynchronous and cannot be undone by resuming. A schedule's pause/resume actions control future triggers, not an already-running workflow. Consumer pause/resume requires a separately implemented and authorized API extension; this documentation does not add it.

## Shared conventions

Use HTTPS outside local development and the installation's configured API origin. Public browser startup routes are `GET /api/v1/health` and `GET /api/v1/auth/config`. Operational `/healthz` and worker `/readyz` probes remain unversioned. Keycloak authentication routes belong to the identity provider.

Studio requires a human access token and, for workflow operations, `X-Runloom-Team`. Consumer requests use a separate execution-audience token and derive the team from the service account. Never put client secrets in a browser. Only consumer starts offer the documented durable `Idempotency-Key` contract.

See [API version policy](workflow-api.md#api-versions), [authentication](authentication.md), and [deployment](deployment.md). The [supplemental Studio guide](studio-api.md) and consumer guide explain lifecycle and retry details alongside the interactive specifications.
