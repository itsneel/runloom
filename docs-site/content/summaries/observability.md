# Logs & metrics

Use logs to explain failures and metrics to track service behavior. Optional logging sidecars and Prometheus are available; no visualizer is bundled.

## Investigate a problem

1. Identify the affected service and request ID.
2. Find matching structured logs across the gateway and downstream services.
3. Check readiness and metrics for errors, latency or dependency failures.
4. Inspect the database or Temporal dependency before restarting anything.

Request correlation links related HTTP calls. It is not a replacement for a workflow/run ID or full distributed tracing.

## Enable collection

Configure sidecars and Prometheus through the deployment values described below. They are optional; metrics endpoints alone do not install collection or alerts.

Keep logs and metrics private. Do not include tokens, secrets, prompts or sensitive payloads in diagnostic requests. Expand the reference for exact Helm/Compose settings and verification commands.
