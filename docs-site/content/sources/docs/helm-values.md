# Configure Runloom with Helm

Use this guide to choose an installation mode and prepare a values file. Defaults below apply to application chart 0.1.0. The [default values](../charts/runloom/values.yaml) list every key, and the [schema](../charts/runloom/values.schema.json) defines accepted types and ranges.

## Choose an installation mode

| Installation | Start with | What you supply |
| --- | --- | --- |
| Local Kind cluster | [Local cluster instructions](deployment.md#development-cluster) | Docker, Kind, kubectl, Helm, Make and Node.js. The helper builds images and provisions local dependencies. A fresh helper installation selects MongoDB unless you choose PostgreSQL. |
| Direct Helm installation for development | Chart defaults with `development.enabled=true` | Images available to cluster nodes, an application database server, service credentials and persistent storage. The bundled PostgreSQL instance serves Keycloak. The application database driver defaults to PostgreSQL. |
| Hosted cluster | Copy the [hosted example](../charts/runloom/values-production.yaml) into your own values file | Set `development.enabled=false`. Replace the example registry, endpoints, hostnames and Secret names. Review the example's enabled ingress, autoscaling and disruption budgets against your cluster. |

## Required resources and values

A default Secret name tells a workload where to read its credentials. Create the corresponding object in the release namespace before starting the workload. Store credentials in Secrets and put only their names and key names in values.

| Values | Required when | What to configure |
| --- | --- | --- |
| `images.<component>.repository`, `tag`; `docs.image` | For every enabled workload | Local image names use the `local` tag. For hosted use, publish the gateway, services, worker and Studio images to a reachable registry. Supply the docs and bootstrap images when used. Set `imagePullSecrets` for registries that require credentials. |
| `database.driver`, `splitServices.databases.<owner>` | Every installation | Choose `postgres` (chart default) or `mongodb`. Set a unique database name and URL Secret for each of `auth`, `workflows` and `management`. The default Secret key is `url`. The worker uses the Workflows database. Keep `splitServices.enabled=true`. |
| `splitServices.credentials`, `splitServices.trustConfigMap` | Every installation | Supply distinct token Secrets for Auth, Workflows, Management and worker, plus the trust ConfigMap. See [service credential requirements](service-split.md#4-helm-and-externally-supplied-databases). |
| `auth.publicURL`, `auth.url`, `auth.issuer`, `auth.jwksURL` | External identity in hosted mode | Set the Studio browser origin and the Keycloak base, issuer and signing-key URLs. The development Studio origin is `http://localhost:4200`. |
| `consumer.provisioning` | Hosted Auth | Set `enabled=true`, `adminURL`, `clientID`, `existingSecret` and its `secretKey`. Auth uses the provisioner for organization membership and service accounts. See [authentication](authentication.md). |
| `temporal.address`, `temporal.namespace`, `temporal.tls` | External Temporal in hosted mode | Set a reachable frontend and an existing bootstrap namespace. Use TLS for hosted connections. Supply the CA Secret for a private CA. For client certificates set both `certKey` and `keyKey` with TLS enabled and an existing Secret. Keep `namespacePrefix` stable across upgrades. |
| `workflowSecrets.existingSecret` or `workflowSecrets.externalSecret.enabled` | Hosted execution | Choose one source for the persistent 32-byte encryption key. The default key name is `encryption-key`. External Secrets Operator also needs a store reference and remote key. Development can generate and retain the key. See [key backup and rotation considerations](../charts/runloom/README.md#workflow-vault-encryption-secret). |

## Optional features and conditional values

| Feature | Chart default | When to change it |
| --- | --- | --- |
| Database setup hook | `databaseSetup.enabled=false` | Enable it to provision service-owned databases on an existing server before install or upgrade. Provide `databaseSetup.existingSecret` and the per-owner password keys. See the [hook prerequisites](../charts/runloom/README.md#database-provisioning-hook). |
| Assistant | `assistant.enabled=true`, `assistant.provider=disabled` | An enabled Assistant service needs its own `databaseSecret` and `databaseURLKey`. To enable model calls, configure its provider, endpoint, model and provider credentials separately from the worker. |
| Workflow AI nodes | `llm.defaultModel` and `llm.existingSecret` empty | Set the endpoint and model for workflows using AI nodes. Reference a provider Secret when authentication is required. Supply `llm.pricingConfigMap` for a custom pricing catalogue. |
| Email nodes | `email.enabled=true`, local Mailpit | Disable email in hosted values or supply an external SMTP host, sender and TLS mode. Supplying `email.username` also requires `email.existingSecret`. |
| Studio routing | `ingress.enabled=false`, `service.uiType=ClusterIP` | For a chart-managed Ingress, set the class and match `ingress.host` to `auth.publicURL`. For TLS, use HTTPS and include the host in the TLS entry. Keep Ingress disabled for port-forwarding or externally managed routes. See [routing modes and overlays](../charts/runloom-ingress/README.md#choose-ownership). |
| Ingress controller | Separate chart, `nginx.enabled=false` | Install the [controller chart](../charts/runloom-ingress/README.md) with this flag enabled only if Runloom should own a controller. An existing controller can serve the application Ingress. |
| Documentation site | `docs.enabled=true`, `docs.ingress.enabled=false` | Give hosted docs their own image and browser origin via `docs.publicURL`. Enable docs Ingress only when the chart should own that route. |
| Scaling and availability | Per-workload autoscaling and disruption budgets disabled | Enable selected entries under `autoscaling.workloads` and `podDisruptionBudgets.workloads`. Autoscaling needs a working metrics API and nonzero requests for the measured resources. |
| Telemetry | `observability.enabled=false` | Enable the collector when collecting telemetry. Review nested Prometheus storage and retention before enabling the stack. See [observability](observability.md). |

## Check values before installing

From the repository root, run the following offline checks with Helm installed. The example renders hosted manifests using placeholder references. Repeat with your own values file before deployment.

```sh
helm lint --strict charts/runloom -f charts/runloom/values-production.yaml
helm template runloom charts/runloom -f charts/runloom/values-production.yaml > /dev/null
helm lint --strict charts/runloom-ingress --set nginx.enabled=true
```

Rendering validates chart inputs. Verify that the referenced Secrets, images, storage and endpoints exist in the intended environment. For installation, health checks and recovery, follow the [deployment guide](deployment.md#helm-chart).
