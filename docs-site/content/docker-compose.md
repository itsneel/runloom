# Run Runloom locally

Start the independent services with Docker Compose, then create your first workflow in Studio. Run these commands from the Runloom repository root, not from the documentation project.

## Before you begin

Install and start Docker with Compose. Install Make and Node.js 26.13.0 (`nvm install` and `nvm use` from the repository root). Allow Docker enough resources for the application services, databases, Keycloak and Temporal.

Use synthetic data. The accounts provisioned here have public local-only passwords and must never be exposed as hosted accounts.

## Start the platform

For MongoDB application storage:

```sh
make local-services-up SERVICES_DATABASE=mongodb
```

For a separate PostgreSQL installation:

```sh
make local-services-up SERVICES_DATABASE=postgres
```

Setup builds the service images, provisions owner-restricted databases and private configuration, and creates local organization, member, platform administrator and staff accounts. Existing credentials and data are preserved.

## Open Studio

Use the URL and account table printed by setup. Default Studio ports are **34200 for MongoDB** and **35200 for PostgreSQL**. A forwarded Kind installation normally uses **4200**, which is a different installation.

For workflow building, use the organization administrator or member account from the terminal. Platform-only accounts open the platform console and do not receive organization membership.

The documentation header's **Open Studio** link defaults to the Kind port. Use the adjacent **Studio connection** control to select your installation.

## Create your first workflow

Continue to [Your first workflow](/guides/first-workflow). You can test a draft without publishing. Tests run real activities, so begin with a simple End workflow node.

## Stop without deleting data

```sh
make local-down SERVICES_DATABASE=mongodb
```

Use `SERVICES_DATABASE=postgres` for the other installation. This stops the selected stack while retaining volumes. Do not delete volumes or the vault encryption key as a troubleshooting shortcut.

For account defaults, private configuration locations and operator commands, see [Local setup reference](/deployment/local-setup).
