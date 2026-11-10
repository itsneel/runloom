#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
node --test scripts/tests/local-mongodb.test.mjs scripts/tests/local-k8s-services.test.mjs
node --test scripts/tests/ingress-chart.test.mjs
helm lint --strict charts/runloom-ingress
helm lint --strict charts/runloom-ingress --set nginx.enabled=true
cmp deploy/keycloak/runloom-realm.json charts/runloom/files/runloom-realm.json
node --test scripts/tests/workflow-key-chart.test.mjs
node --test scripts/tests/service-chart.test.mjs
node --test scripts/tests/assistant-chart.test.mjs
node --test scripts/tests/docs-deployment.test.mjs
node --test scripts/tests/scaling-chart.test.mjs
node --test scripts/tests/security-chart.test.mjs
node --test scripts/tests/observability-chart.test.mjs
helm lint --strict charts/runloom
helm lint --strict charts/runloom --set database.driver=mongodb
helm lint --strict charts/runloom -f charts/runloom/values-production.yaml
helm template check charts/runloom >/dev/null
helm template check charts/runloom --set workflowSecrets.existingSecret=runloom-workflow-key >/dev/null
helm template check charts/runloom --set llm.pricingConfigMap=runloom-pricing --set email.host=smtp.example.test --set email.username=runloom --set email.existingSecret=runloom-smtp >/dev/null
if helm template check charts/runloom --set email.host=smtp.example.test --set email.tls=none >/dev/null 2>&1; then
  echo "ERROR: chart accepted external plaintext SMTP" >&2; exit 1
fi
if helm template check charts/runloom --set email.host=smtp.example.test --set email.username=runloom >/dev/null 2>&1; then
  echo "ERROR: chart accepted SMTP authentication without a Secret" >&2; exit 1
fi
runloom_services=$(helm template runloom charts/runloom --show-only templates/services.yaml)
case "$runloom_services" in
  *"name: runloom-api"*"name: runloom-ui"*) ;;
  *) echo "ERROR: default Runloom Service names are incorrect" >&2; exit 1 ;;
esac
helm template check charts/runloom --set database.driver=mongodb >/dev/null
helm template check charts/runloom -f charts/runloom/values-production.yaml --set database.driver=mongodb --set llm.existingSecret=runloom-llm >/dev/null
helm template check charts/runloom --set consumer.provisioning.enabled=true --set consumer.provisioning.adminURL=http://keycloak:8080/auth --set consumer.provisioning.existingSecret=runloom-provisioner >/dev/null
if helm template check charts/runloom --set consumer.provisioning.enabled=true >/dev/null 2>&1; then
  echo "ERROR: chart accepted incomplete service-account provisioning" >&2
  exit 1
fi
if helm template check charts/runloom --set database.driver=file >/dev/null 2>&1; then
  echo "ERROR: chart accepted unsupported filesystem storage" >&2
  exit 1
fi
if helm template check charts/runloom --set development.enabled=false >/dev/null 2>&1; then
  echo "ERROR: chart accepted missing external service configuration" >&2
  exit 1
fi
if helm template check charts/runloom -f charts/runloom/values-production.yaml --set splitServices.databases.auth.existingSecret= >/dev/null 2>&1; then
  echo "ERROR: chart accepted missing Auth database Secret reference" >&2
  exit 1
fi
if helm template check charts/runloom -f charts/runloom/values-production.yaml --set auth.issuer= >/dev/null 2>&1; then
  echo "ERROR: chart accepted missing hosted identity issuer" >&2
  exit 1
fi
if helm template check charts/runloom --set auth.realm=custom >/dev/null 2>&1; then
  echo "ERROR: chart accepted a development realm mismatch" >&2
  exit 1
fi
echo "Helm development, external service and invalid configuration checks passed."
