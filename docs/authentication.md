# Accounts, organizations, and team access

Runloom uses Keycloak for login, organization names, hierarchy, membership and organization roles. Auth verifies signed OIDC tokens and reads the organization directory from Keycloak. Application storage holds product team-access bindings, platform permissions, policies and audit history, rather than another organization identity directory. Passwords, Google credentials and reset tokens never enter workflow definitions.

Backend integrations use separate [service accounts and the consumer execution API](consumer-api.md), with client credentials, an execution-specific audience, explicit version grants, one-time secret display and immediate application revocation. The human login verifier remains separate; service-account tokens cannot access Studio or management routes.

## Local start

For a ready-to-use local owner account, run `make local-up` and open http://localhost:14200. See [local administrator setup](local-development.md) for the public development login, repeatable bootstrap, and explicit password reset. The manual registration flow below remains available for the ordinary Compose stack.

1. Run `make docker-up` and open http://localhost:4200 (use `localhost`, not `127.0.0.1`; OAuth origins must match).
2. Studio opens the Runloom-branded login page directly. Choose **Create an account**, register, then open the verification message at http://localhost:8025. Mailpit captures development email; it does not deliver real mail.
3. Follow the verification link. Create an organization; you become its owner and receive a General team.
4. Open **Workspace settings → Organization** to add organization members by email, then use **Teams & members** to create teams and assign members. Pending members gain access after signing in with that verified email. This version does not send invitation emails; share the studio URL separately.
5. Use **Forgot password?** on the welcome screen or the Keycloak login form. Follow the reset email and choose a new password. Responses do not disclose whether an account exists.

Keycloak administration is at http://localhost:4200/auth/admin/. The local bootstrap account is `admin` / `development-only`. These are public development credentials, never production credentials. Identity data uses a separate PostgreSQL volume, including when workflow data uses MongoDB. Mailpit messages are ephemeral. `make docker-down` retains identity and workflow volumes.

For native Go/Angular development, start dependencies with `docker compose --project-directory . -f compose/compose.yaml up -d --wait identity-db mailpit keycloak`, then use the existing native API/worker/Angular commands. The Angular proxy forwards `/auth` to `localhost:18081`. Keep the default public origin `http://localhost:4200` unless also updating OAuth configuration.

## Roles and isolation

| Scope | Role | Permissions |
| --- | --- | --- |
| Organization | Owner | Manage organization memberships, including owners; create teams; admin access to every team |
| Organization | Admin | Manage non-owner memberships and create teams; admin access to every team |
| Organization | Member | Access only explicitly assigned teams; cannot create additional organizations |
| Team | Admin | Manage team memberships and all workflow operations |
| Team | Editor | Read, design, commit, publish, restore, start, approve, signal, resume, and cancel workflows |
| Team | Viewer | Read definitions, commits, run inputs/results, and audit history; cannot mutate or run |

Only an owner can grant or remove ownership. An organization must retain at least one joined owner; a pending owner invitation cannot replace the last joined owner. Team admins can assign only existing organization members. Removing an organization member removes their team assignments. Permission checks happen server-side on every request; hiding UI buttons is not the security boundary. Roles are fixed in this version, not custom role definitions or per-node approval policies.

Editors and viewers can search and view their own team's roster, including owners/admins who inherit team access from the organization. Role changes, additions and removals remain admin-only. Inherited access is read-only in the team roster and is managed at organization level. Members cannot read another team's roster or the organization-wide member directory.

Organizations may be independent or children of one root organization. A child cannot contain another child. A verified user with no memberships can create their first organization; afterwards they can create children within that family only, when they are an owner/admin of its parent. An account cannot create a second independent parent or receive memberships or pending invitations in another family through Runloom. Creating beneath a parent also requires owner/admin membership in that parent. Team administration alone does not qualify.

Open **Management → Organization** to view the hierarchy, rename the selected organization, or create another. Children can see parent and sibling names and IDs; switching and data access still require explicit membership. The default is two children plus their parent. Under **Platform console → Organizations**, platform administrators can configure 0–10 children per parent. Lowering the limit preserves existing children and blocks additional creation. Restricted staff and organization admins cannot change this policy.

Platform administrators can also choose **Edit limit** beside a parent to override its child limit with any value from 0–10. Zero blocks new children. **Use platform default** removes the override, so future default changes apply again. The table shows child count, effective limit and its source; Studio uses the same effective limit. Overrides may be above or below the default, but never above ten. Existing children are preserved. Policy writes and resets are audited and stored as Runloom policy metadata in either application database; organization identity stays in Keycloak. Child organizations cannot receive a child-limit override.

The workspace organization switcher stays within the selected organization family and lists only explicit memberships there. The creation form offers only that family's root when you administer it. Unrelated parents and their children are excluded from both controls. The independent-organization option is offered only to accounts without memberships or pending invitations. Membership checks use both the verified subject ID and email; moving to another family requires removing existing memberships and invitations first. Existing operator-created grants are not automatically deleted.

Keycloak administrators can manage the same organizations under **Organizations** and override Runloom creation constraints. Attributes `runloom.parent_id`, `runloom.owners` and `runloom.admins` carry the parent ID and organization-role user IDs; native Keycloak membership is also required for access. `runloom.id` is the permanent Runloom ID: do not edit it. Renaming preserves that ID and the Temporal namespace. Pending email grants are kept in `runloom.pending_members` until a verified account accepts them. Organization visibility never grants inherited workflow or secret access.

Startup migrates the previous application organization directory into Keycloak, preserving Runloom organization/team IDs and existing access bindings. The live application directory is removed after successful migration. Back up Keycloak and application storage before upgrading. The provisioner needs organization administration and user/membership access (`manage-realm`, `manage-users`, `view-users`, `query-users` on Keycloak 26.4), in addition to its existing client-management roles. Local Compose/Kind setup grants these automatically without rotating credentials; hosted operators must configure them. The provisioner credential stays server-side in Auth and the explicit local administrator Job.

Enable native Organizations in the Keycloak realm. Local setup disables only the optional **Organization Identity-First Login** execution so the existing email/password, social login and MFA flow stays intact. For hosted realms, configure the browser flow to match your login experience; organization membership is read server-side and does not require an identity-first login screen.

Workflow requests require `Authorization: Bearer <access-token>` and `X-Runloom-Team: <team-id>`. The token must have the configured issuer, audience and authorized party, RS256 signature, unexpired lifetime, verified email, and access-token type. User-provided actor fields/headers are replaced with the authenticated subject for workflow actions and commits. Only `/healthz`, `/api/v1/health`, and `/api/v1/auth/config` are public.

Callbacks, named signals, run controls, and webhook starts also require a team editor/admin token. Previously anonymous integrations must be updated. Machine credentials, signed public webhook URLs, replay protection, and rate limiting are not implemented by this change. Do not distribute a user's refresh token to integrations.

Each team has separate PostgreSQL schemas, MongoDB collection prefixes, or filesystem directories. API run access is checked against that team's records before any Temporal lookup or signal. Temporal run IDs include the team ID. This is application-level isolation: the shared worker, database credential, and Temporal namespace remain trusted infrastructure. Do not expose Temporal or databases publicly.

Existing unscoped workflow data is preserved but is not assigned to a new team automatically. It will not appear in the authenticated studio. Back up and explicitly migrate selected records with an operator-reviewed mapping; do not expose legacy records to the first user who registers.

## Google login

The shared login page displays providers actually enabled in Keycloak. Google cannot work until the OAuth client below is configured. The Studio `RUNLOOM_GOOGLE_LOGIN_ENABLED` flag controls only the optional shortcut on the fallback sign-in screen, not the provider configuration or the shared login form.

1. Create an OAuth web client in your Google Cloud project and configure its consent screen and allowed users as appropriate.
2. In Keycloak's `runloom` realm, choose **Identity providers → Google**, use alias `google`, and configure the Google client ID and secret there. Keep the secret out of the Angular app, application environment, and workflow JSON.
3. Register the exact redirect URI displayed by Keycloak with Google. Locally it is `http://localhost:4200/auth/realms/runloom/broker/google/endpoint`; hosted URLs must use your actual identity origin and HTTPS.
4. Enable the provider and retain Keycloak's verified-email/account-linking safeguards. Never enable silent linking of accounts merely because an unverified email matches.
5. Set `RUNLOOM_GOOGLE_LOGIN_ENABLED` to `true` in the YAML selected by `RUNLOOM_API_CONFIG_PATH` for Compose (or use the environment setting for native API without mounted configuration), or `auth.googleEnabled=true` in Helm, then recreate the API. This enables **Continue with Google**, which redirects through Keycloak. Google accounts must provide a verified email.

The repository does not contain Google credentials. Google login needs operator configuration and cannot be exercised end-to-end without a configured OAuth client. See [Keycloak identity brokering](https://www.keycloak.org/docs/latest/server_admin/#_identity_broker) for provider and account-linking settings.

## Hosted identity and email

### Runloom login appearance and navigation

The login theme lives in `charts/runloom/files/keycloak-theme/runloom/`. Compose mounts it read-only and the development Helm deployment mounts the same files from a ConfigMap. New realms select `loginTheme: runloom`. Existing realms are not overwritten on startup: after installing the theme, select **Realm settings → Themes → Login theme → runloom** in Keycloak administration and save. Do not delete the identity database or re-import the realm to change its theme.

For externally managed production Keycloak, include this directory as `/opt/keycloak/themes/runloom` in your identity deployment or image and select the theme there. The Runloom production chart does not manage external Keycloak. The theme inherits Keycloak's maintained form templates and scripts; custom files provide CSS, English messages, and a footer only. Login, registration, verification, password recovery, MFA challenges, and error pages therefore retain their original form actions and security behavior. Recheck visual and authentication tests when upgrading Keycloak. No external fonts, analytics, or third-party assets are loaded by the theme.

Studio uses authorization code with S256 PKCE and requests login directly when no session exists. Existing sessions bypass the form. The redirect is derived from the current Studio URL, retains its path/query/fragment, strips OAuth response fields, and never follows a caller-supplied return URL. This preserves URL-addressable destinations; it does not add new project/workflow routes or persist unsaved editor state. Signing out first ends the Keycloak session, then returns to Studio, which automatically opens the branded login form without another click. Legacy `/?signedOut=1` links also open login. Passwords remain in Keycloak; Google still uses Google's account chooser. Google secrets belong only in Keycloak.

Run `npm --prefix studio/libraries/platform test` for redirect checks. With the browser variables described below set, `make test-auth-theme` also checks live desktop/tablet/mobile login, registration, recovery, invalid credentials, and signed-out navigation. Run the existing `scripts/auth-smoke.mjs` against a disposable local stack for registration, email verification, organization ownership, workflow execution, and password recovery. It creates synthetic records. Test Google with a real configured provider separately; enabling a UI flag is not a Google login test.

Run `make test-keycloak-admin-live` to verify local administrator login and navigation into the Runloom realm. The proxy preserves Keycloak's framing and Content Security Policy headers on `/auth/`; Studio retains `X-Frame-Options: DENY`. Keycloak uses [session-check iframes](https://www.keycloak.org/securing-apps/javascript-adapter#_session_status_iframe), so inheriting Studio's frame denial breaks its admin console. See [NGINX header inheritance](https://nginx.org/en/docs/http/ngx_http_headers_module.html#add_header). To test the Compose proxy image with an isolated upstream, set `RUNLOOM_TEST_PROXY_IMAGE` to the built host image and run `npm --prefix studio/projects/host test`.

Provision a production Keycloak deployment separately; bundled Keycloak `start-dev`, bootstrap credentials, SMTP without TLS, and Mailpit are development-only. Configure HTTPS, restricted administration, database backups, trusted reverse proxies, brute-force protection, email delivery, and operational monitoring.

Use [deploy/keycloak/runloom-realm.json](../deploy/keycloak/runloom-realm.json) as a starting point, replacing development SMTP settings and setting exact application origins. Set its `RUNLOOM_PUBLIC_URL` and `RUNLOOM_SMTP_HOST` import variables. Configure an SMTP provider with TLS and credentials in Keycloak for verification/recovery. The public `runloom` client uses authorization code with S256 PKCE, no implicit/password grants, the `basic` scope for the user subject, and an access-token audience mapper for `runloom`. Retain these settings. The browser keeps access/refresh tokens in memory, not localStorage; only selected org/team IDs are saved there. See the [Keycloak JavaScript adapter](https://www.keycloak.org/securing-apps/javascript-adapter).

| Native API variable | Helm value | Purpose |
| --- | --- | --- |
| `RUNLOOM_OIDC_URL` | `auth.url` | Browser-accessible Keycloak base URL; `/auth` locally |
| `RUNLOOM_OIDC_ISSUER` | `auth.issuer` | Exact public realm issuer from discovery |
| `RUNLOOM_OIDC_JWKS_URL` | `auth.jwksURL` | API-reachable signing-key endpoint; may use private routing |
| `RUNLOOM_OIDC_CLIENT_ID` | `auth.clientID` | Public client and access-token audience; default `runloom` |
| `RUNLOOM_OIDC_REALM` | `auth.realm` | Realm name; default `runloom` |
| `RUNLOOM_GOOGLE_LOGIN_ENABLED` | `auth.googleEnabled` | Show configured Google login |
| `RUNLOOM_PUBLIC_URL` (Compose) | `auth.publicURL` | Exact browser origin without trailing slash for development realm import |

Hosted Helm requires `development.enabled=false` and the external auth URL, issuer, and JWKS URL. Configure the external client's allowed redirect/logout URLs and web origin to match the UI. The sample `values-production.yaml` contains placeholders. HTTPS is required for hosted PKCE browser use. An external Keycloak URL is accessed directly by the browser; the UI proxies `/auth` only in development.

Development Helm includes Keycloak, its PostgreSQL StatefulSet, and Mailpit. With the default release name, view emails using `kubectl -n runloom port-forward svc/runloom-mailpit 8025:8025`. Set `auth.publicURL` to the exact origin used for the studio. Default realm/client names are fixed to `runloom` in development. Imported realms are created only when absent; editing the import or public URL does not update existing clients. Update existing Keycloak settings explicitly instead of deleting identity volumes. See [Keycloak import behavior](https://www.keycloak.org/server/importExport).

## Management API

For starting workflows, checking status, and cancellation, see the [workflow execution API reference](workflow-api.md).

All routes below require a valid user token; org/team permissions are checked from the route scope, not client claims.

| Route | Purpose |
| --- | --- |
| `GET /api/v1/studio/me` | Current profile, organizations, accessible teams, effective roles |
| `POST /api/v1/studio/orgs` | Create with `{ "name": "Example" }` |
| `POST /api/v1/studio/orgs/{org}/teams` | Create team with `{ "name": "Engineering" }` |
| `GET/POST /api/v1/studio/orgs/{org}/members` | List or add/change `{ "email": "person@example.com", "role": "member" }` |
| `DELETE /api/v1/studio/orgs/{org}/members/{email}` | Remove organization membership and team assignments |
| `GET/POST /api/v1/studio/orgs/{org}/teams/{team}/members` | List or assign a team role |
| `DELETE /api/v1/studio/orgs/{org}/teams/{team}/members/{email}` | Remove explicit team assignment |
| `GET /api/v1/studio/orgs/{org}/audit` | Organization admin/owner membership audit |

Team rename/deletion, organization deletion, invitation email delivery, custom roles and SCIM are not included. Keycloak handles credential lifecycle; deleting/disabling an identity there does not delete application data. Human signed access tokens remain valid until expiry (five minutes by default), while application membership revocation applies to subsequent requests immediately. Consumer service accounts additionally check their local active status and credential generation on every request; rotate/revoke through Runloom for immediate consumer-token invalidation.

Database-backed identity, secrets, and service-account metadata use separate revision scopes and normalized entity/event records. Updates commit atomically within the owning scope; authorization uses a tenancy read projection. A single audit-history document no longer grows toward MongoDB's document limit, but owner-aggregate reads, per-organization membership sizes, retention, and tenant quotas still need capacity planning. Each API-loaded team owns a connection pool; worker pools are reused with a 16-team bound. Filesystem mode supports one API process only. See [fresh-install storage](database.md#fresh-install-storage-layout).

## Validation

Run `make test-go` for unit tests. Set disposable `RUNLOOM_TEST_POSTGRES_URL` and `RUNLOOM_TEST_MONGODB_URL` when running Go tests to exercise actual membership persistence and team data isolation; the tests create synthetic records.

For the existing workflow smoke test, sign in as an editor/admin, capture the short-lived access token from your own browser's token exchange into a permission-restricted temporary file, and use the team ID from `/api/v1/studio/me`:

```bash
RUNLOOM_ACCESS_TOKEN_FILE=/absolute/private/access-token RUNLOOM_TEAM_ID=your-team-id make smoke
```

Do not paste tokens into shell history, logs, or committed files. Remove the private file after testing. This manual token is only for local diagnostics; do not disable PKCE or enable password grants for tests.

`scripts/auth-smoke.mjs` automates registration, email verification, org/team creation, the workflow smoke, logout, and password recovery against a disposable local stack. Install Playwright into a temporary tool directory, set `RUNLOOM_PLAYWRIGHT_MODULE` to its `index.mjs`, and set `RUNLOOM_CHROME` to your Chrome executable. Set `RUNLOOM_UI_URL` and `RUNLOOM_MAILPIT_URL` for non-default ports, then run `node scripts/auth-smoke.mjs` from the repository root. It creates synthetic accounts and workflow records, never prints credentials, and does not exercise Google. Mailpit must be private because its emails include live action links.
