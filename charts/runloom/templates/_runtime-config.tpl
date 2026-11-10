{{- define "runloom.runtimeConfig" -}}
{{- $ := .root -}}
{{- $component := .component -}}
TEMPORAL_ADDRESS: {{ include "runloom.temporalAddress" $ | quote }}
TEMPORAL_NAMESPACE: {{ $.Values.temporal.namespace | quote }}
RUNLOOM_TASK_QUEUE: {{ $.Values.temporal.taskQueue | quote }}
RUNLOOM_DATABASE_DRIVER: {{ $.Values.database.driver | quote }}
RUNLOOM_DATABASE_URL_FILE: /var/run/runloom-database/url
{{ if include "runloom.workflowSecret" $ -}}
RUNLOOM_SECRETS_KEY_FILE: /var/run/runloom-workflow-secrets/encryption-key
{{ end -}}
{{ if eq $component "api" -}}
RUNLOOM_API_ADDRESS: "0.0.0.0:8080"
RUNLOOM_EXECUTION_AUDIENCE: {{ $.Values.consumer.audience | quote }}
RUNLOOM_OIDC_CLIENT_ID: {{ $.Values.auth.clientID | quote }}
RUNLOOM_OIDC_REQUIRED_ACR: {{ $.Values.auth.requiredACR | quote }}
RUNLOOM_OIDC_MAX_AUTH_AGE: {{ $.Values.auth.maxAuthAge | quote }}
RUNLOOM_SENSITIVE_RUN_ROLES: {{ join "," $.Values.executionSecurity.sensitiveRunRoles | quote }}
RUNLOOM_OIDC_REALM: {{ $.Values.auth.realm | quote }}
RUNLOOM_GOOGLE_LOGIN_ENABLED: {{ $.Values.auth.googleEnabled | quote }}
{{ if $.Values.development.enabled -}}
RUNLOOM_OIDC_URL: /auth
RUNLOOM_OIDC_ISSUER: {{ printf "%s/auth/realms/runloom" (trimSuffix "/" $.Values.auth.publicURL) | quote }}
RUNLOOM_OIDC_JWKS_URL: {{ printf "http://%s-keycloak:8080/auth/realms/runloom/protocol/openid-connect/certs" (include "runloom.fullname" $) | quote }}
{{ else -}}
RUNLOOM_OIDC_URL: {{ required "auth.url is required for hosted deployments" $.Values.auth.url | quote }}
RUNLOOM_OIDC_ISSUER: {{ required "auth.issuer is required for hosted deployments" $.Values.auth.issuer | quote }}
RUNLOOM_OIDC_JWKS_URL: {{ required "auth.jwksURL is required for hosted deployments" $.Values.auth.jwksURL | quote }}
{{ end -}}
{{ if $.Values.consumer.provisioning.enabled -}}
RUNLOOM_KEYCLOAK_ADMIN_URL: {{ required "consumer.provisioning.adminURL is required" $.Values.consumer.provisioning.adminURL | quote }}
RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID: {{ $.Values.consumer.provisioning.clientID | quote }}
RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE: /var/run/runloom-provisioner/client-secret
{{ end -}}
{{ else -}}
RUNLOOM_WORKER_HEALTH_ADDRESS: "0.0.0.0:8081"
RUNLOOM_LLM_BASE_URL: {{ $.Values.llm.baseURL | quote }}
RUNLOOM_LLM_DEFAULT_MODEL: {{ $.Values.llm.defaultModel | quote }}
RUNLOOM_LLM_PROVIDER: {{ $.Values.llm.provider | quote }}
{{ if $.Values.llm.pricingConfigMap -}}
RUNLOOM_LLM_PRICING_FILE: /var/run/runloom-pricing/pricing.json
{{ end -}}
{{ if $.Values.llm.existingSecret -}}
RUNLOOM_LLM_API_KEY_FILE: /var/run/runloom-llm/api-key
{{ end -}}
{{ if $.Values.email.enabled -}}
{{ if and $.Values.development.enabled (not $.Values.email.host) -}}
{{ if $.Values.email.username }}{{ fail "Development Mailpit does not accept SMTP authentication; configure email.host" }}{{ end -}}
RUNLOOM_SMTP_HOST: {{ printf "%s-mailpit" (include "runloom.fullname" $) | quote }}
RUNLOOM_SMTP_PORT: "1025"
RUNLOOM_SMTP_TLS: none
{{ else -}}
{{ if eq $.Values.email.tls "none" }}{{ fail "External SMTP must use starttls or tls" }}{{ end -}}
RUNLOOM_SMTP_HOST: {{ required "email.host is required outside development Mailpit" $.Values.email.host | quote }}
RUNLOOM_SMTP_PORT: {{ $.Values.email.port | quote }}
RUNLOOM_SMTP_TLS: {{ $.Values.email.tls | quote }}
{{ end -}}
RUNLOOM_SMTP_FROM: {{ $.Values.email.from | quote }}
RUNLOOM_SMTP_ALLOWED_DOMAINS: {{ join "," $.Values.email.allowedDomains | quote }}
{{ if $.Values.email.username -}}
{{ if not $.Values.email.existingSecret }}{{ fail "email.existingSecret is required with username" }}{{ end -}}
RUNLOOM_SMTP_USERNAME: {{ $.Values.email.username | quote }}
RUNLOOM_SMTP_PASSWORD_FILE: /var/run/runloom-smtp/password
{{ end -}}
{{ end -}}
{{ end -}}
{{- end -}}
