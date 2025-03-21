{{/*
Expand the name of the chart.
*/}}
{{- define "rasberrypi-pico-driver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "rasberrypi-pico-driver.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Allow the release namespace to be overridden for multi-namespace deployments in combined charts
*/}}
{{- define "rasberrypi-pico-driver.namespace" -}}
  {{- if .Values.namespaceOverride -}}
    {{- .Values.namespaceOverride -}}
  {{- else -}}
    {{- .Release.Namespace -}}
  {{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "rasberrypi-pico-driver.chart" -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" $name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "rasberrypi-pico-driver.labels" -}}
helm.sh/chart: {{ include "rasberrypi-pico-driver.chart" . }}
{{ include "rasberrypi-pico-driver.templateLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Template labels
*/}}
{{- define "rasberrypi-pico-driver.templateLabels" -}}
app.kubernetes.io/name: {{ include "rasberrypi-pico-driver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Values.selectorLabelsOverride }}
{{ toYaml .Values.selectorLabelsOverride }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "rasberrypi-pico-driver.selectorLabels" -}}
{{- if .Values.selectorLabelsOverride -}}
{{ toYaml .Values.selectorLabelsOverride }}
{{- else -}}
{{ include "rasberrypi-pico-driver.templateLabels" . }}
{{- end }}
{{- end }}

{{/*
Full image name with tag
*/}}
{{- define "rasberrypi-pico-driver.fullimage" -}}
{{- .Values.image.repository -}}:{{- .Values.image.tag | default .Chart.AppVersion -}}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "rasberrypi-pico-driver.serviceAccountName" -}}
{{- $name := printf "%s-service-account" (include "rasberrypi-pico-driver.fullname" .) }}
{{- if .Values.serviceAccount.create }}
{{- default $name .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use for the webhook
*/}}
{{- define "rasberrypi-pico-driver.webhookServiceAccountName" -}}
{{- $name := printf "%s-webhook-service-account" (include "rasberrypi-pico-driver.fullname" .) }}
{{- if .Values.webhook.serviceAccount.create }}
{{- default $name .Values.webhook.serviceAccount.name }}
{{- else }}
{{- default "default-webhook" .Values.webhook.serviceAccount.name }}
{{- end }}
{{- end }}
