# Studio API

Use these APIs for human-authorized authoring, management and run operations.

## Make a request

1. Sign in interactively and use a short-lived human access token.
2. Call `GET /api/v1/studio/me` to discover accessible teams.
3. Send `Authorization: Bearer <access-token>` and the required `X-Runloom-Team` header for team-scoped operations.
4. Use the endpoint reference below for schemas and required roles.

Existing-workflow edits need an edit lease. Production starts require a published main workflow; draft testing uses the separate test endpoint.

## Know the boundaries

Service-account tokens are rejected here. A team header selects context; it does not grant access.

Draft tests run real activities. Resume continues an eligible paused run; cancellation is permanent. Do not retry a failed start blindly—check its retry contract first.

The embedded reference is read-only. **Open installed Swagger** opens your real installation for authenticated testing.
