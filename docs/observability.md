# Logging sidecars and API metrics

Runloom can run an OpenTelemetry Collector alongside each Go service and worker, with Prometheus for metrics storage and its HTTP query API. No Grafana, Loki, Runloom dashboard, tracing backend or customer-facing performance API is installed. The built-in Prometheus diagnostic endpoints are operator tools, not a product visualizer.

## Collection and privacy

1. Auth, Workflows, Management, Gateway and Assistant (Lumi) record completed request counts, duration histograms in seconds and active requests. Lumi's SSE connections remain streaming: active requests include open streams, and duration/completion are recorded when a stream closes. These are HTTP metrics, not model token usage or time-to-first-token. The worker records its health HTTP traffic and lifecycle, not Temporal workflow/activity performance. Instrumentation is outside workflow execution and replay.
2. Applications send OTLP over `127.0.0.1:4318` to a collector within their network namespace. The collector exposes Prometheus metrics on port 8889. Each replica is scraped separately; counters are not load-balanced through a service. Gateway totals are separate from downstream service totals: do not sum both to calculate external traffic.
3. Operational logs cover service start/stop and HTTP completion events. Logs contain only fixed fields: service, generated request ID, method, registered route template or bounded route family, response status and duration. Request IDs are not metric labels. Inputs, outputs, prompts, secrets, headers, query strings, error bodies and identities are excluded. Existing legacy application diagnostics remain on stdout/stderr and are not automatically forwarded by this SDK. Security audits retain their existing independent durable storage.
4. Unmatched/pre-routing requests use a bounded route family. Go ServeMux routes use templates such as `POST /api/v1/runs/{id}`. The gateway has route-family metrics, not every downstream endpoint. Metrics contain no organization, team, run, user or client identifiers. HTTP request IDs correlate internal service calls as described below; distributed tracing is not installed.
5. Log queues hold 1,024 records and exports have one-second timeouts without retries. Shutdown flush is bounded to three seconds. A missing collector or unavailable sink does not block HTTP handling, but telemetry can be lost. Kubernetes container failures can still affect overall pod readiness. This is not an audit/compliance guarantee.
6. Collector files rotate at 10 MB with three backups, inside a 64 MiB ephemeral volume. Logs are lost when that volume/pod is removed; they are not a centralized searchable log store. Sampled detailed records also appear in collector stdout for diagnostics. Prometheus defaults to seven days or 3 GB retention, whichever limit is reached, with a 5 GiB retained PVC on Helm or a named Compose volume. Leave storage headroom when overriding limits.

## Local Compose

```sh
make local-services-up SERVICES_DATABASE=mongodb SERVICES_OBSERVABILITY=1
node scripts/test-observability.mjs
```

For PostgreSQL use `SERVICES_DATABASE=postgres`; Studio is on port 35200 and Prometheus on 39091. MongoDB uses Studio 34200 and Prometheus 39090. Both Prometheus ports bind only to localhost. Set `RUNLOOM_UI_URL` and `RUNLOOM_PROMETHEUS_URL` accordingly for the verification script. Keep `SERVICES_OBSERVABILITY=1` for subsequent up/down commands for that stack. Omitting it disables export in newly generated application configuration but does not remove previously created companions; stop them using the observability-enabled down command. Data volumes are preserved.

Six companion containers, including Assistant, share their corresponding application's network namespace using `network_mode: service:...`. They do not mount application credentials, databases, payload encryption keys or Docker sockets. Collector configuration is in `compose/config/collector.yaml`; Prometheus configuration is in `compose/config/prometheus.yaml`. Local runtime settings remain mounted files, not telemetry secret environment variables. Collector files in Compose are memory-backed and ephemeral.

## Helm

```sh
make k8s-upgrade K8S_OBSERVABILITY=1
kubectl --context kind-runloom-local -n runloom port-forward svc/runloom-prometheus 9090:9090
RUNLOOM_UI_URL=http://localhost:4200 RUNLOOM_PROMETHEUS_URL=http://localhost:9090 node scripts/test-observability.mjs
```

Studio also needs its normal `make k8s-run` connection. The upgrade flag enables `observability.enabled`; subsequent upgrades preserve it. Normal chart defaults keep collection disabled. Hosted installations set `observability.enabled: true` explicitly. No Prometheus Operator or CRDs are required: namespaced pod discovery uses a dedicated ServiceAccount with only pod get/list/watch permission. Prometheus is ClusterIP-only with no ingress. NetworkPolicies restrict metrics ingress to the bundled Prometheus and deny pod-originated Prometheus ingress; they require a policy-capable CNI. Local Kind's default networking must not be treated as evidence of policy enforcement. Operators can use authenticated Kubernetes port-forwarding.

When telemetry is enabled, backend HPAs use `ContainerResource` for the application container, excluding sidecar resource usage. Metrics Server is still required for HPA; this Prometheus installation does not replace it or install a custom-metrics adapter. Sidecars have separate CPU/memory limits and read-only root filesystems.

Set `observability.collectorConfigMap` to an existing ConfigMap containing `config.yaml` to customize collectors, for example to export logs to an external OTLP destination. Keep the localhost receiver, health endpoint and metrics endpoint consistent. The override is operator-managed; restart deployments after changing it. No backend is provisioned for external logs. If export needs authentication, provide a separately secured collector deployment/configuration; application secrets must not be reused. Set `observability.prometheus.enabled: false` when another Prometheus is responsible for scraping, and provide an explicit NetworkPolicy allowance for that scraper.

## HTTP request correlation

1. The public Gateway generates a fresh `X-Runloom-Request-ID` for every request, replacing caller-supplied values. It returns that ID in the response, including errors. Internal services preserve a single canonical 32-character lowercase hexadecimal ID, replacing missing, malformed, duplicate or all-zero values. IDs are diagnostic metadata, never proof of identity or permission.
2. Internal HTTP clients propagate the context ID only to their configured Runloom origin. This covers Gateway forwarding, Auth/Workflows/Management service RPCs, Lumi's membership checks and Lumi's workflow-context/authoring calls. Cross-origin redirects remove the header. No global transport is changed, and customer API calls, LLM providers and Keycloak clients do not receive this Runloom header.
3. Correlation remains enabled when telemetry export is disabled. When collection is enabled, each completed request log contains the shared `request_id`; metric labels never contain request IDs. Use the response header from browser developer tools or `curl -i` to locate the same ID across services' collector logs.
4. This is HTTP-chain correlation, not W3C tracing, spans or durable job tracing. Queued authoring jobs and Temporal activities do not inherit the originating browser request ID. Background internal HTTP requests without an existing context start a new chain. Request IDs do not expose prompts, message text, credentials or user identity.

For example, read Lumi's collector logs in Kind:

```sh
kubectl --context kind-runloom-local -n runloom logs deployment/runloom-assistant -c telemetry --since=5m
```

Collector stdout contains sampled detailed records; its rotating file exporter contains the exported records until rotation or pod removal. Readiness/liveness requests contribute metrics but are omitted from HTTP completion logs.

## Metrics API

Use `GET /api/v1/query` or `/api/v1/query_range` on the **internal Prometheus service**, not Runloom's public gateway. Examples:

```promql
sum by (component) (rate(runloom_http_requests_total[5m]))
sum by (component) (rate(runloom_http_requests_total{http_response_status_code=~"5.."}[5m]))
histogram_quantile(0.95, sum by (le, component, http_route) (rate(http_server_request_duration_seconds_bucket[5m])))
sum by (component) (http_server_active_requests)
up{job="runloom"}
```

Metrics describe API response time, not the time for an asynchronous workflow to complete. Health probes are included in metrics and should be filtered out when measuring user traffic. Histograms are aggregatable across replicas; counts reset on application restart. Telemetry gaps are unknown data, not zero errors. Restrict the query API to platform operators: there is no Runloom tenant authorization layer on Prometheus itself.

## Verification and SDK ownership

`sdk/go-observability` is an independent Go module. Services consume its pinned v0.2.0 vendor snapshot, not sibling source. Run its race tests, each affected service's tests, `make check-service-boundaries`, and `make helm-lint`. `scripts/test-observability.mjs` checks six scrape targets (including Lumi), requests, latency, active requests, gateway request-ID replacement, and exclusion of synthetic sensitive markers and IDs from metrics against a running local stack. It makes deliberately unauthorized requests without accessing user data.

Verification on 2026-10-21:

1. SDK race tests cover concurrent three-hop correlation, spoofed/duplicate/malformed IDs, exact-origin forwarding, cross-origin redirect stripping, collector outages, and payload-free OTLP export. Gateway and Lumi race tests pass, including single-valued response headers and SSE flushing with telemetry enabled.
2. All six isolated offline Go project builds/tests pass. Five independent backend Docker images built successfully. Helm lint/regression tests and Compose configuration validation pass for MongoDB and PostgreSQL. Lumi's persistence, jobs and streaming suite passes against disposable databases of both types.
3. Local Kind runs the updated images and six collector sidecars. The live Prometheus check passes for all six targets, request counts, latency, active requests, unauthorized responses and exclusion of request IDs/payload markers from metrics.
4. Authenticated browser login and read-only requests verified matching IDs in rotating collector files across Gateway → Lumi → Auth, Gateway → Management → Auth, Gateway → Workflows → Auth, and Gateway → Lumi → Workflows → Auth. The MongoDB PVC was retained. No paid model was invoked.
5. The live cluster uses MongoDB. PostgreSQL was verified with isolated Lumi tests and deployment configuration checks, not a second full Compose or Kubernetes rollout. Durable job tracing and Temporal execution tracing remain outside this HTTP-correlation implementation.

References: [OpenTelemetry agent pattern](https://opentelemetry.io/docs/collector/deploy/agent/), [HTTP metrics](https://opentelemetry.io/docs/specs/semconv/http/http-metrics/), [Prometheus HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/).

The rotating file exporter is an [upstream alpha component](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.162.0/exporter/fileexporter/README.md). Its JSON is diagnostic output, not a stable application API or durable audit format.
