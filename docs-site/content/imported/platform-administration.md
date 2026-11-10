# Platform administration

Implemented contract: 2026-10-21. Platform administration is separate from organization and team membership. Keycloak authenticates users; Runloom Auth owns platform assignments and enforces platform operations.

## Roles and boundaries

| Role | Allowed operations |
| --- | --- |
| Platform admin | Read service status and organization metadata; assign/revoke platform roles; read platform access audit |
| Platform staff | Read service status and organization metadata only |
| Organization/team owner or admin | No platform access unless separately assigned |

Organization metadata contains only ID, name, team count and member count. Neither platform role grants access to customer workflow definitions, prompts, chats, secrets or run payloads. Existing tenant memberships still apply independently. There is no impersonation or database editor. Service accounts cannot use platform APIs.

## Separate Angular application

`studio/projects/platform-admin` is an independent Angular/Webpack application with its own package/lockfile, tests, build, distribution and Dockerfile. The host loads it through runtime federation, using the existing versioned session library. Platform features are not compiled into the host or Management app.

Open `/platform` or the Platform console navigation button. Canonical pages are `/platform/health`, `/platform/organizations`, `/platform/staff` and `/platform/audit`. Unauthorized direct links show an access-restricted page, while APIs independently return 403. The console does not require selecting an organization/team and does not mount Lumi.

Platform staff and administrators signing in from `/` or the default `/workflows` landing page go directly to `/platform/health`. Explicit deep links are preserved. Initial workspace rendering waits for the platform identity lookup to avoid briefly showing the workflow dashboard. The Platform console entry uses the same sidebar rows, collapsed icons and tooltips as Workflows and Projects.

## Setup and authentication

1. `make local-services-up SERVICES_DATABASE=mongodb` or `SERVICES_DATABASE=postgres` provisions dedicated `platform-admin@runloom.test` and `platform-staff@runloom.test` accounts. Their public local-only default passwords are `RunloomPlatformAdmin!123` and `RunloomPlatformStaff!123`. Kind setup uses the same dedicated bootstrap image. Neither account receives tenant memberships. The organization administrator and team member remain separate accounts.
2. Setup grants the dedicated platform accounts their roles once, with an atomic audit marker. Subsequent setup preserves their passwords, role changes and revocations, including when `local-admin-reset` resets passwords. The tenant account `admin@runloom.test` is an organization administrator only: local setup removes its legacy platform grant with an audit event after confirming that the dedicated platform administrator has active access. If that prerequisite is missing, setup fails without removing the legacy grant or restoring revoked dedicated access. Organization/team roles remain unchanged. Existing accounts with conflicting identity subjects cause setup to fail rather than being rebound. Public defaults are never provisioned by hosted Helm installs.
3. For hosted setup, first sign in with a verified user so its subject exists in Auth. Build the Auth-owned setup executable with `make -C server/auth build`, then run `RUNLOOM_CONFIG_FILE=/secure/auth/config.yaml server/auth/bin/runloom-local-admin --platform-admin-subject <subject>`. This explicit operator operation requires Auth database credentials. It neither creates an identity nor changes organization access; it refuses to replace an existing administrator. Do not expose this command through HTTP.
4. Hosted platform endpoints fail closed until `platformAccess.requiredACR` identifies a Keycloak authentication context backed by an MFA flow. `platformAccess.maxAuthAge` defaults to `15m`; older sessions must sign in again. Merely naming an ACR does not configure MFA in Keycloak. The application verifies the signed context and authentication time using its existing token SDK. No automatic step-up redirect is added in this release.
5. Development Helm and local Compose explicitly enable `RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY=true`. Never enable this override in a hosted deployment. Hosted YAML uses `RUNLOOM_PLATFORM_REQUIRED_ACR` and optionally `RUNLOOM_PLATFORM_MAX_AUTH_AGE`.

## Staff lifecycle and storage

An admin supplies an existing verified user's email, desired role and a reason in Staff & access. Empty role revokes platform access. The resulting grant is keyed by the stored identity subject, not by a token role claim. Every request rereads permissions, so revocation applies to the next request using an already issued token. An in-flight read may complete if it was authorized before revocation.

Role changes and their actor, target, previous/new roles, reason and UTC timestamp are committed atomically in the Auth identity scope. Both MongoDB and PostgreSQL use the existing normalized recordset repository and compare-and-swap transaction. Concurrent requests cannot remove all assigned platform administrators. Platform audit events are separate from organization audit events; staff cannot read them. The last-admin invariant concerns Runloom assignments, not whether a Keycloak user was disabled independently.

Existing records require no destructive migration: absent platform fields mean no access. Deploy Auth instances consistently before enabling role mutations; older Auth writers do not understand the new fields. Rollback to an older writer is not safe after platform assignments have been created. No automatic conversion of existing organization roles is performed.

## Health and API contract

The console checks Auth's identity database and the configured Workflows, Worker, Management and Lumi readiness endpoints. Gateway is checked for process responsiveness to avoid using aggregate dependency status as a substitute for per-service results. Targets are fixed by operator configuration, never supplied by the browser. Upstream response bodies, URLs and secrets are not exposed. Missing configuration is reported as unknown; failed readiness is degraded; a transport failure is unavailable. Checks time out after three seconds and run concurrently. Health checks do not establish LLM provider availability or complete workflow correctness.

The health page refreshes every 30 seconds while visible. It remains dependent on Runloom's gateway and identity services and is not an independent outage monitor. A separate external status system remains future work.

Platform OpenAPI: `/api/v1/platform/openapi.json`. Interactive reference: `/api/docs/platform/`. Auth owns `/api/v1/platform/me`, `/organizations`, `/health`, `/staff` (GET/PUT) and `/audit`. Lists use `limit` (1–100, default 50) and `offset`; they return `items`, `total`, `offset`, `limit`. Audit sorts newest first. Other lists sort by ID. Pagination is not a snapshot across concurrent changes. Protected endpoints require a human Studio bearer token and platform permission, without `X-Runloom-Team`. Invalid input or last-admin removal returns 400, authentication failure 401, denied role/MFA 403, unavailable storage 503.

## Verification

1. `make test-platform-admin` runs the independent Angular source-boundary and state tests; `make build-platform-admin` builds it independently.
2. Start one local database stack, then run `make test-platform-live SERVICES_DATABASE=mongodb` or `SERVICES_DATABASE=postgres`. The database test creates and drops a uniquely named test schema/database. Browser tests temporarily grant the demo member restricted staff access and revoke it in cleanup; they refuse to overwrite a pre-existing platform assignment.
3. `make test-services-live SERVICES_DATABASE=<driver>` checks existing cross-service workflow behavior. `make test-admin-setup-live SERVICES_DATABASE=<driver>` verifies repeat bootstrap preserves login, membership and workflow records.
4. `make helm-lint`, `make check-service-boundaries` and `make test-docker-images` validate packaging and independent builds. Staging deployment is a separate process.

## Future platform capabilities (not included)

1. Operational health history, incident banners and independently hosted outage reporting.
2. Organization lifecycle controls, quotas, usage summaries and billing metadata, without exposing customer content.
3. Fine-grained staff permission bundles, temporary grants, approval workflows and emergency access with expiry.
4. Security posture reporting: MFA coverage, inactive accounts, credential-expiry metadata and audit exports.
5. Configuration change history, feature rollout controls and backup/restore verification. Privileged changes should require previews, explicit confirmation and audited results.

Direct SQL consoles, unrestricted impersonation, secret-value retrieval and generic Kubernetes-admin access are intentionally excluded.
