# Troubleshooting

Start with the affected layer. Avoid resetting databases or deleting private configuration as a first step.

## The page opens but login fails

Confirm that you opened the Studio URL printed by your installation, not the documentation server. Compose MongoDB, Compose PostgreSQL and Kind use different local ports. Check Keycloak availability and the configured browser redirect origin in [authentication](/management/access).

## Services are not ready

Read the affected container's logs and Kubernetes events where applicable. Connection refused means a process is not listening at that address; it is not proof that the endpoint path is wrong. Database readiness, credentials and startup errors need separate checks. See [observability](/operations/observability) and [deployment](/deployment/helm).

## A draft cannot be published

Correct node validation errors and check whether another editor holds the workflow lease. Saving an invalid draft is allowed; publishing requires a valid saved definition. Published child references need valid versions. See [editing](/guides/workflow-builder) and [versioning](/guides/versioning).

## A connection test fails

Read the detailed connection error. Check the database host reachable from the worker/API, credentials, TLS settings and the MongoDB authentication database. Private hosts may require operator approval. See [database connections](/integrations/connections).

## Lumi has no model response

Configure the assistant provider in application configuration. With no model configured, chat can still persist messages and report that the LLM is not configured. See [Lumi configuration and guardrails](/guides/lumi). Never put provider credentials into workflow JSON or documentation URLs.

## An API returns access denied

Verify the interface and credential type: [Consumer](/api/consumer), [Studio](/api/studio), or [Platform](/api/platform). Then check the appropriate team, grant or platform permission. A documentation page being public does not make its operations public.
