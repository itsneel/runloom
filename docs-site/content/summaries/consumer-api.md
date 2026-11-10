# Consumer API

Use service accounts for backend integrations, not for browser sign-in.

## Start an integration

1. Create a service account with narrow scopes and explicit workflow/version grants.
2. Store its one-time-visible secret securely.
3. Obtain a Keycloak client-credentials token for the execution API audience.
4. List granted workflows, start a published main workflow, then inspect or cancel your own runs.

Use a stable idempotency key when retrying the same start request. Follow the request and retry contract below; a new key can create new work.

## Access boundaries

Tokens require the appropriate `workflows:read`, `runs:start`, `runs:read` or `runs:cancel` scope and grants. Run visibility is consumer-owned, not every run in the team.

Child workflows cannot start directly. Consumer pause/resume and Studio management operations are not supported. Quotas and redaction apply.

Use the OpenAPI controls below for exact requests and schemas. **Open installed Swagger** sends real requests to your selected installation.
