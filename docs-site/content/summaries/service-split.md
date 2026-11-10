# Service architecture

Runloom services build independently and communicate through authenticated HTTP contracts.

## Who owns what?

| Component | Responsibility |
| --- | --- |
| Keycloak | Login and tokens |
| Auth | Organizations, teams, roles and service accounts |
| Workflows + worker | Definitions, execution, run history and secrets |
| Management | Prompts, budgets and usage accounting |
| Assistant | Private conversations and authoring jobs |
| Gateway | Route public API requests; no database |

Each owner has a separate MongoDB or PostgreSQL database and credentials. The worker shares only the Workflows database. Services do not read another owner's database or import sibling source.

## Deployment boundaries

Four independent Studio apps share a host-managed session at runtime. Documentation has a separate build and public origin.

Local setup provisions the service resources automatically. Hosted deployment needs owner-restricted databases, workload credentials and private encrypted service transport. See [deployment](/deployment/helm).
