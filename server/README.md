# Runloom services

1. Auth: `auth/`, identity directory, team permissions and service-account lifecycle. Keycloak authenticates users and applications.
2. Workflows: `workflows/`, definitions, runs, secrets and the Temporal worker.
3. Management: `management/`, prompts, budgets and LLM usage analytics.
4. Assistant: `assistant/`, private conversations, streaming responses and persistent planning jobs; its own PostgreSQL or MongoDB database. See [assistant setup](../docs/assistant.md).
5. Gateway: `gateway/`, stateless public API routing to owner services; no database.

Each directory is an independent Go module and Docker context. There is no root Go module or Go workspace. Services communicate using versioned HTTP APIs, not Go imports or another service's database. The three stateful services support MongoDB and PostgreSQL. The SDK is maintained independently at `../sdk/go-auth/`; database provisioning tooling is at `../tools/database-setup/`.

Run `make local-services-up SERVICES_DATABASE=mongodb` or `make local-services-up SERVICES_DATABASE=postgres` from the repository root for a complete local installation. Root Make targets only orchestrate these independent builds.
