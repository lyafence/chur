{{- define "chur.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "chur.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "chur.labels" -}}
helm.sh/chart: {{ include "chur.name" . }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "chur.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "chur.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chur.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "chur.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-webhook" (include "chur.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "chur.webhook.serviceName" -}}
{{ printf "%s-webhook" (include "chur.fullname" .) }}.{{ .Release.Namespace }}.svc
{{- end }}

{{- define "chur.webhook.namespaceSelector" -}}
namespaceSelector:
  matchExpressions:
  {{- if .Values.webhook.allowedNamespaces }}
    - key: kubernetes.io/metadata.name
      operator: In
      values:
        {{- toYaml .Values.webhook.allowedNamespaces | nindent 8 }}
  {{- end }}
    - key: kubernetes.io/metadata.name
      operator: NotIn
      values:
        {{- range .Values.webhook.skipNamespaces }}
        - {{ . }}
        {{- end }}
        {{- if not (has .Release.Namespace .Values.webhook.allowedNamespaces) }}
        - {{ .Release.Namespace }}
        {{- end }}
{{- end }}

{{- define "chur.webhook.tlsChecksum" -}}
{{- if eq .Values.tls.provider "helmGenerated" }}
{{- $secret := lookup "v1" "Secret" .Release.Namespace .Values.tls.userSecret.name }}
{{- if $secret }}
{{- $secret.data | toJson | sha256sum }}
{{- else }}
{{- .Values.tls.userSecret.name | sha256sum }}-helmGenerated
{{- end }}
{{- else }}
{{- .Values.tls.userSecret.name | sha256sum }}-{{ .Values.tls.provider | sha256sum }}
{{- end }}
{{- end }}
