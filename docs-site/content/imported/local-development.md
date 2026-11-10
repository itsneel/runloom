# Local startup and administrator

Updated: 2026-10-21. The backend runs as independent Auth, Workflows, Management and Assistant services, a stateless gateway and the Workflows worker. The monolithic server is no longer a startup option.

## Start the product

```sh
make local-services-up SERVICES_DATABASE=mongodb
# Or a separate PostgreSQL installation:
make local-services-up SERVICES_DATABASE=postgres
```

`make local-up`, `make fresh-up` and `make docker-up` use the same independent-service setup. They preserve existing service volumes and credentials; “fresh” does not delete data. MongoDB is the default selection. The older monolith's volumes are not read or migrated.

1. MongoDB Studio: http://localhost:34200/; gateway: http://localhost:38080/; Temporal: http://localhost:38233/.
2. PostgreSQL Studio: http://localhost:35200/; gateway: http://localhost:39080/; Temporal: http://localhost:39234/.
3. Public local-only logins: `admin@runloom.test` / `RunloomLocal!123` and `member@runloom.test` / `RunloomMember!123`. Both belong to the Runloom organization and General team. The admin is an organization owner and team admin; the member starts with organization `member` and team `editor` roles, allowing workflow authoring/testing without membership administration. Neither is a Keycloak administrator. Existing passwords and the member's existing roles are preserved.
4. Dedicated platform logins: `platform-admin@runloom.test` / `RunloomPlatformAdmin!123` and `platform-staff@runloom.test` / `RunloomPlatformStaff!123`. Open `/platform`. The admin manages platform access and audits; staff can only view service health and organization metadata. Neither receives organization/team membership. These are public local-only defaults, not hosted credentials. Existing passwords, platform role changes and revocations are preserved.

Setup builds each image from its own project, starts dependencies, provisions owner-restricted databases, verifies cross-database denial, seeds all four local accounts, and configures Keycloak service-account provisioning before starting the product. MongoDB uses an authenticated replica set. Keycloak has its own PostgreSQL database under either selection. Startup stops if any account fails to provision rather than reporting a healthy installation.

Local setup also removes any legacy platform grant from `admin@runloom.test` after confirming the dedicated platform administrator has active access. That account remains an organization owner/team administrator and is not listed in platform Staff & access. This correction is audited and does not change its password or tenant memberships.

Compose startup/admin setup and Kind startup/open print all four accounts in one terminal table with public default passwords, roles, organization and team. Studio and Platform console URLs appear below the table. A shared note distinguishes preserved passwords from an explicit reset; displayed defaults are not a claim about a previously changed password.

## Configuration and credentials

Generated configuration is mounted YAML. Private files live under `~/.config/runloom/local/services/runloom-services-<database>/`, outside the repository. Each service receives only its own database and workload credential. Auth alone receives the Keycloak provisioner credential. Workflows and its worker receive the persistent, automatically generated 32-byte vault key. The gateway receives no credentials.

Credential files are reused on subsequent starts; setup does not rotate them or overwrite account passwords. The private directory is mode 0700 and its files are mode 0640, with the host file group supplied to non-root containers. Secret values are not printed. Back up credentials and the workflow key separately from databases. Deleting the key is not rotation: saved secrets require their original key.

## Commands

```sh
make local-config SERVICES_DATABASE=mongodb
make local-admin SERVICES_DATABASE=mongodb
make local-admin-reset SERVICES_DATABASE=mongodb
make docker-logs SERVICES_DATABASE=mongodb
make local-down SERVICES_DATABASE=mongodb
```

`local-config` creates or reuses private configuration and validates Compose without starting containers. `local-admin` ensures all four local accounts while preserving their passwords; `local-admin-reset` explicitly restores all four public local passwords and ends their existing sessions. The member's existing roles and all existing platform access decisions are preserved even during password reset. Setup recreates missing default tenant memberships, but never restores a revoked platform role. Disable the bootstrap rather than using it for production accounts. `local-down` stops only the selected service installation and retains data. Neither database selection touches unrelated Docker projects or Kubernetes.

## Independent development and verification

Run `make build`, `make test`, `make race`, `make vet` or `make docker-build` within `server/auth`, `server/workflows`, `server/management` or `server/gateway`. Each has its own module, vendored dependencies and binary output directory. The SDK in `sdk/go-auth` has its own build and tests. See [service architecture](service-split.md) for HTTP boundaries, native configuration and Helm prerequisites.

```sh
make build-server test-server test-auth-sdk
make check-service-boundaries
make test-services-live SERVICES_DATABASE=mongodb
make test-services-live SERVICES_DATABASE=postgres
```

The browser checks need Playwright and a browser. Set `RUNLOOM_PLAYWRIGHT_MODULE` to the installed Playwright module and optionally `RUNLOOM_CHROME` to a local Chrome executable. Live tests create synthetic workflows, runs and teams, then exercise the actual service boundary with local Temporal and a fake LLM. They do not call paid providers or send external email.

Kubernetes is a separate installation process. Run `make k8s-up` for automatic local kind setup, then `make k8s-open` to launch the website/Studio. The helper provisions the database server, owner-restricted service databases, workload-token Secrets, trust ConfigMap, Keycloak provisioner and all four local accounts described above. No values file is required. MongoDB is the default; use `K8S_DATABASE=postgres` for a fresh PostgreSQL installation. `make k8s-upgrade` preserves existing data and credentials. `K8S_SERVICE_VALUES` remains an optional operator-managed override, not a prerequisite. Follow [Helm setup](../charts/runloom/README.md). Staging deployment is always separate; local setup never starts it.
