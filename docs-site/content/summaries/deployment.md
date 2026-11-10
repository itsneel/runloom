# Deployment & Helm

Use the local helper for development. Hosted environments need reviewed infrastructure and credentials.

## Run locally

From the repository root, with Docker, Kind, kubectl, Helm, Make and supported Node.js installed:

```sh
make k8s-up
make k8s-run
```

Setup provisions the local databases, credentials and development accounts. Use the printed Studio URL. In another terminal, `make k8s-docs` opens a separate documentation port. Keep forwarding terminals running.

Use `make k8s-upgrade` to rebuild and upgrade. Existing passwords and data are preserved; changing database drivers is not a migration.

## Before hosting

Supply independent service database Secrets, workload credentials, Keycloak, Temporal, vault keys, image tags and HTTPS routing. Back up keys separately from databases. Never expose public development accounts.

Run `make helm-lint` and `make helm-template` to check configuration without deploying. Expand the reference for required values, commands, security limitations and recovery guidance.
