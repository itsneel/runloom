{{- define "runloom.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "runloom.publicPort" -}}
{{- $url := urlParse .Values.auth.publicURL -}}
{{- default (ternary "443" "80" (eq $url.scheme "https")) (trimPrefix ":" (regexFind ":[0-9]+$" $url.host)) -}}
{{- end -}}
{{- define "runloom.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 48 | trimSuffix "-" -}}
{{- else if contains (include "runloom.name" .) .Release.Name -}}
{{- .Release.Name | trunc 48 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "runloom.name" .) | trunc 48 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- define "runloom.labels" -}}
app.kubernetes.io/name: {{ include "runloom.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}
{{- define "runloom.temporalAddress" -}}
{{- if .Values.development.enabled -}}
{{- printf "%s-temporal:7233" (include "runloom.fullname" .) -}}
{{- else -}}
{{- required "temporal.address is required when development.enabled=false" .Values.temporal.address -}}
{{- end -}}
{{- end -}}
{{- define "runloom.serviceAccount" -}}
{{- if .root.Values.serviceAccount.create -}}
{{- printf "%s-%s" (default (include "runloom.fullname" .root) .root.Values.serviceAccount.name) .component -}}
{{- else -}}
{{- default "default" .root.Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
{{- define "runloom.workflowSecret" -}}
{{- $external := .Values.workflowSecrets.externalSecret | default dict -}}
{{- if and $external.enabled .Values.workflowSecrets.existingSecret -}}
{{- fail "workflowSecrets.existingSecret and externalSecret.enabled are mutually exclusive" -}}
{{- end -}}
{{- if .Values.workflowSecrets.existingSecret -}}
{{- .Values.workflowSecrets.existingSecret -}}
{{- else if $external.enabled -}}
{{- default (printf "%s-workflow-key" (include "runloom.fullname" .)) $external.targetName -}}
{{- else if .Values.development.enabled -}}
{{- printf "%s-workflow-key" (include "runloom.fullname" .) -}}
{{- end -}}
{{- end -}}
