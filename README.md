# Runloom

Build, publish, and run durable workflows with a visual editor and a Go runtime powered by Temporal. Runloom Studio brings together workflow development, human approvals, integrations, and execution history.

[Website](https://itsneel.github.io/runloom/) · [Product overview](docs/product.md) · [Examples](examples/README.md) · [Releases](https://github.com/itsneel/runloom/releases) · [Contributing](CONTRIBUTING.md)

Website publication is pending as of 2026-11-18. The install command below downloads directly from GitHub.

## What you can build

1. **Visual workflows** with conditions, parallel branches, timers, callbacks, named signals, and human approvals. [Workflow contract](docs/json-contract.md)
2. **Connected processes** using [HTTP APIs and secrets](docs/api-nodes-and-secrets.md), [databases](docs/database-connections.md), [email](docs/email-node.md), and [JavaScript](docs/custom-code.md).
3. **AI-assisted workflows** with Lumi for creating and editing drafts, plus AI nodes for execution. Model configuration is required. [Lumi setup and usage](docs/assistant.md)
4. **Reusable automation** with schedules, version-pinned subworkflows, and immutable published versions. [Schedules and subworkflows](docs/schedules-and-subworkflows.md) · [Version history](docs/versioning.md)

## Quick start

### Install a published release locally

Requires **Docker running**, **kind**, **kubectl**, **Helm**, **Node.js**, **curl**, and **tar**. Run from any directory, replacing `0.1.0` with your chosen [release version](https://github.com/itsneel/runloom/releases).

```bash
curl -fsSL https://raw.githubusercontent.com/itsneel/runloom/master/scripts/install-version.sh | bash -s -- 0.1.0
```

The [installer](scripts/install-version.sh) creates or reuses the `runloom-rc` kind cluster, installs the published Runloom chart with PostgreSQL, and prints development login details. Open **http://localhost:4210** while the command is running. Ctrl+C stops forwarding and leaves the cluster and data intact. Reusing an installation applies the selected release to it.

Append `--no-forward` to exit after installation checks. Use `bash scripts/install-version.sh --help` from a checkout for cluster, port, and timeout options. This setup is for local development.

### Install on your Kubernetes cluster

Prepare the databases, credentials, identity provider, and Temporal service described in the [Helm values guide](docs/helm-values.md), then supply your configuration:

```bash
helm upgrade --install runloom oci://ghcr.io/itsneel/charts/runloom \
  --version 0.1.0 \
  --namespace runloom --create-namespace \
  --values runloom-values.yaml
```

`runloom-values.yaml` is your prepared values file. Review the [deployment guide](docs/deployment.md) before hosting and the [upgrade guide](docs/upgrading.md) before changing an existing installation. Bundled local services and development accounts are not production defaults to rely on.

### Develop from source

With Git, Make, Node.js/npm, and Docker Compose available:

```bash
git clone https://github.com/itsneel/runloom.git
cd runloom
make local-services-up SERVICES_DATABASE=postgres
```

Setup builds and starts the services and prints the Studio URL and local account details. Use `SERVICES_DATABASE=mongodb` for a separate MongoDB installation. See [local service setup](docs/service-split.md#3-fresh-local-setup) for prerequisites, configuration, and shutdown, and [CONTRIBUTING.md](CONTRIBUTING.md) for native builds and checks.

## Documentation

| I want to… | Read |
| --- | --- |
| Build and test a workflow | [Builder guide](docs/workflow-editing.md) · [Runnable examples](examples/README.md) · [JSON schema](spec/workflow.schema.json) |
| Integrate an external application | [Consumer API](docs/consumer-api.md) · [API overview](docs/api.md) |
| Manage access and projects | [Authentication](docs/authentication.md) · [Projects](docs/projects.md) · [Platform administration](docs/platform-administration.md) |
| Inspect execution and AI usage | [Runs and workflow actions](docs/workflow-api.md) · [LLM usage and estimated costs](docs/llm-costs.md) |
| Operate an installation | [Deployment](docs/deployment.md) · [Helm configuration](docs/helm-values.md) · [Upgrades](docs/upgrading.md) · [Storage](docs/database.md) |
| Understand or contribute code | [Architecture](docs/architecture.md) · [Service boundaries](docs/service-split.md) · [Contribution guide](CONTRIBUTING.md) |

Studio and consumer APIs use different authentication and permissions. Their interactive references are available at `/api/docs/studio/` and `/api/docs/consumer/` on your installation's Studio/API origin.

## Repository

| Component | Location |
| --- | --- |
| Go services and Temporal worker | [server/](server/) |
| Runloom Studio | [studio/](studio/README.md) |
| Independent product website | [Website/](Website/README.md) |
| Documentation website | [docs-site/](docs-site/README.md) |
| Workflow contracts and examples | [spec/](spec/) · [examples/](examples/README.md) |
| Kubernetes charts | [charts/runloom/](charts/runloom/README.md) |

## License

Runloom's original code, documentation, schemas, and examples are licensed under [Apache 2.0](LICENSE). Dependencies and bundled services retain their own licenses. See [third-party notices](THIRD_PARTY_NOTICES.md).
