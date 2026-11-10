# Runloom Go authentication SDK

Independent Go module for verifying Keycloak access tokens. It has no Runloom server imports, database connection, permission cache or login endpoints.

Create one verifier per issuer, audience and identity kind using `runloomauth.New`, then call `Verify(ctx, token)`. Use `Bearer(request)` to reject ambiguous Authorization headers. Human tokens require the configured Studio client and a verified email. Service-account tokens require the execution audience, service identity claim and credential generation. Both paths verify RS256 signatures, issuer, audience, expiry, subject, access-token type and not-before time. JWKS fetching is cached with a bounded HTTP timeout.

The SDK authenticates identity only. Callers must still enforce team/workflow/run permissions, scopes, current account status and generation, including revocation. Never treat a valid JWT as permission to every resource. Use HTTPS for issuer/JWKS endpoints outside isolated local development.

Optional `Config.RequiredACR` requires an exact signed `acr` claim for human tokens. `Config.MaxAuthAge` requires a positive, non-future `auth_time` within the configured duration. Missing claims fail closed when required. These checks do not enroll MFA or implement a browser step-up flow: configure the corresponding Keycloak authentication flow and claim mapping first. Machine identities continue to use their dedicated credentials/scopes, not human MFA claims. Runloom services expose these checks through `RUNLOOM_OIDC_REQUIRED_ACR` and `RUNLOOM_OIDC_MAX_AUTH_AGE` in mounted YAML.

Run independently:

```sh
go test -race ./...
go vet ./...
```

Each service pins version `v0.1.0` and carries an independent vendor snapshot. No service uses a local module replacement, parent workspace or sibling SDK source during builds. This change does not publish the SDK remotely. Release and upgrade the SDK explicitly; regenerate and review each consumer's vendor snapshot when adopting a new version. Never regenerate dependencies as a hidden prerequisite of a normal build.
