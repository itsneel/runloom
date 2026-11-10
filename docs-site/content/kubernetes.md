# Run Runloom in Kubernetes

Use the repository's local Kind setup for a separate Kubernetes installation. These commands run from the Runloom repository root.

## Before you begin

Install Docker, Kind, kubectl, Helm, Make and the repository-supported Node.js version. Start Docker and ensure its available resources can accommodate the stack.

## Create and install

```sh
make k8s-up
```

For a fresh installation with PostgreSQL application storage:

```sh
make k8s-up K8S_DATABASE=postgres
```

Local setup provisions service databases, credential Secrets, trust configuration, Keycloak provisioning and the dedicated local accounts. Use the deployment guide for operator-managed environments; local defaults are not production credentials.

## Open the application

```sh
make k8s-open
```

The helper establishes local access, prints connection information and local account defaults, and opens the product website. Follow its Studio link. Existing account passwords may differ from printed defaults because repeat setup preserves passwords.

## Upgrade safely

```sh
make k8s-upgrade
```

The helper preserves the existing installation's database selection and credentials. Do not switch an existing installation's database driver as an implicit migration.

## Inspect a problem

```sh
kubectl --context kind-runloom-local -n runloom get pods
```

Use your configured context and namespace if you changed the defaults. Inspect events and the affected service's logs before restarting resources. Avoid deleting database volumes or keys.

See [Deployment & Helm](/deployment/helm), [Logs & metrics](/operations/observability) and [Troubleshooting](/operations/troubleshooting). Staging deployment remains a separate, explicitly authorized process.
