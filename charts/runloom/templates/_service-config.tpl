{{- define "runloom.deploymentConfig" -}}
{{- $ := .root -}}
{{- $component := .component -}}
{{- if not $.Values.splitServices.enabled -}}
{{- fail "The monolith was removed. splitServices.enabled must be true; provision the three service databases and credentials before installing." -}}
{{- else -}}
{{- $baseComponent := "api" -}}
{{- if eq $component "worker" }}{{- $baseComponent = "worker" }}{{- end -}}
{{- $c := fromYaml (include "runloom.runtimeConfig" (dict "root" $ "component" $baseComponent)) -}}
{{- if eq $component "api" -}}
{{- $c = dict "RUNLOOM_API_ADDRESS" "0.0.0.0:8080" -}}
{{- if $.Values.assistant.enabled -}}
{{- $_ := set $c "RUNLOOM_ASSISTANT_URL" (printf "http://%s-assistant:8080" (include "runloom.fullname" $)) -}}
{{- end -}}
{{- else -}}
{{- $owner := $component -}}{{- if eq $component "worker" }}{{- $owner = "workflows" }}{{- end -}}
{{- $_ := set $c "RUNLOOM_DATABASE_NAME" (required (printf "splitServices.databases.%s.name is required" $owner) (index $.Values.splitServices.databases $owner).name) -}}
{{- $_ := set $c "RUNLOOM_SERVICE_TOKEN_FILE" "/var/run/runloom-service/token" -}}
{{- $_ := set $c "RUNLOOM_SERVICE_TRUST_FILE" "/var/run/runloom-trust/trust.json" -}}
{{- if not (has $component (list "workflows" "worker")) }}{{- $_ := unset $c "RUNLOOM_SECRETS_KEY_FILE" }}{{- end -}}
{{- if has $component (list "workflows" "worker") -}}
{{- $_ := set $c "RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES" (join "," $.Values.databaseConnections.allowedAddresses) -}}
{{- $_ := set $c "TEMPORAL_NAMESPACE_PREFIX" (default (include "runloom.fullname" $) $.Values.temporal.namespacePrefix) -}}
{{- $_ := set $c "TEMPORAL_RETENTION_DAYS" (toString $.Values.temporal.retentionDays) -}}
{{- if $.Values.temporal.tls.enabled -}}
{{- $_ := set $c "TEMPORAL_TLS_ENABLED" "true" -}}
{{- $_ := set $c "TEMPORAL_TLS_SERVER_NAME" $.Values.temporal.tls.serverName -}}
{{- if $.Values.temporal.tls.existingSecret -}}
{{- if $.Values.temporal.tls.caKey }}{{- $_ := set $c "TEMPORAL_TLS_CA_FILE" "/var/run/runloom-temporal-tls/ca.crt" }}{{- end -}}
{{- if $.Values.temporal.tls.certKey -}}
{{- $_ := set $c "TEMPORAL_TLS_CERT_FILE" "/var/run/runloom-temporal-tls/tls.crt" -}}
{{- $_ := set $c "TEMPORAL_TLS_KEY_FILE" "/var/run/runloom-temporal-tls/tls.key" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_ := set $c "RUNLOOM_ALLOW_LEGACY_PAYLOADS" (toString $.Values.executionSecurity.allowLegacyPayloads) -}}
{{- if $.Values.executionSecurity.existingKeyringSecret -}}
{{- $_ := set $c "RUNLOOM_PAYLOAD_KEYRING_FILE" "/var/run/runloom-payload-keys/keyring.json" -}}
{{- end -}}
{{- end -}}
{{- if ne $component "auth" -}}
{{- range $key := list "RUNLOOM_KEYCLOAK_ADMIN_URL" "RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID" "RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE" }}{{- $_ := unset $c $key }}{{- end -}}
{{- end -}}
{{- if and (eq $component "auth") $.Values.development.enabled (not $.Values.consumer.provisioning.enabled) -}}
{{- $_ := set $c "RUNLOOM_LOCAL_ORGANIZATION_BOOTSTRAP_PENDING" "true" -}}
{{- end -}}
{{- if has $component (list "auth" "management") -}}
{{- range $key := list "TEMPORAL_ADDRESS" "TEMPORAL_NAMESPACE" "RUNLOOM_TASK_QUEUE" }}{{- $_ := unset $c $key }}{{- end -}}
{{- end -}}
{{- end -}}
{{- $_ := set $c "RUNLOOM_AUTH_URL" (printf "http://%s-auth:8080" (include "runloom.fullname" $)) -}}
{{- $_ := set $c "RUNLOOM_WORKFLOWS_URL" (printf "http://%s-workflows:8080" (include "runloom.fullname" $)) -}}
{{- $_ := set $c "RUNLOOM_MANAGEMENT_URL" (printf "http://%s-management:8080" (include "runloom.fullname" $)) -}}
{{- if eq $component "auth" -}}
{{- $_ := set $c "RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY" (toString $.Values.development.enabled) -}}
{{- $_ := set $c "RUNLOOM_PLATFORM_REQUIRED_ACR" $.Values.platformAccess.requiredACR -}}
{{- $_ := set $c "RUNLOOM_PLATFORM_MAX_AUTH_AGE" $.Values.platformAccess.maxAuthAge -}}
{{- $_ := set $c "RUNLOOM_GATEWAY_URL" (printf "http://%s-api:8080" (include "runloom.fullname" $)) -}}
{{- $_ := set $c "RUNLOOM_WORKER_URL" (printf "http://%s-worker:8081" (include "runloom.fullname" $)) -}}
{{- if $.Values.assistant.enabled -}}
{{- $_ := set $c "RUNLOOM_ASSISTANT_URL" (printf "http://%s-assistant:8080" (include "runloom.fullname" $)) -}}
{{- end -}}
{{- end -}}
{{- if $.Values.observability.enabled -}}
{{- $_ := set $c "RUNLOOM_TELEMETRY_ENDPOINT" "http://127.0.0.1:4318" -}}
{{- end -}}
{{- toYaml $c -}}
{{- end -}}
{{- end -}}
