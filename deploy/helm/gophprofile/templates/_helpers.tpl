{{- define "gophprofile.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "gophprofile.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "gophprofile.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gophprofile.name" (index . 0) }}
app.kubernetes.io/component: {{ index . 1 }}
{{- end }}

{{- define "gophprofile.labels" -}}
helm.sh/chart: {{ include "gophprofile.chart" (index . 0) }}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/instance: {{ (index . 0).Release.Name }}
app.kubernetes.io/managed-by: {{ (index . 0).Release.Service }}
{{- end }}

{{- define "gophprofile.waitForSchemaInitContainer" -}}
{{- if .Values.migration.enabled }}
- name: wait-for-migrations
  image: "{{ .Values.postgres.image.repository }}:{{ .Values.postgres.image.tag }}"
  imagePullPolicy: {{ .Values.postgres.image.pullPolicy }}
  command:
    - sh
    - -c
    - |
      until psql "$DATABASE_DSN" -v ON_ERROR_STOP=1 -tAc \
        "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = {{ .Values.migration.targetVersion }} AND dirty = false);" \
        2>/dev/null | grep -q t; do
        sleep 1
      done
  env:
    - name: DATABASE_DSN
      valueFrom:
        secretKeyRef:
          name: {{ .Values.secrets.name }}
          key: DATABASE_DSN
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop:
        - ALL
  resources:
    {{- toYaml .Values.migration.waitForSchema.resources | nindent 4 }}
{{- end }}
{{- end }}

{{- define "gophprofile.tracingEgressPort" -}}
{{- $endpoint := required "config.tracing.otlpEndpoint is required when tracing is enabled" .Values.config.tracing.otlpEndpoint -}}
{{- $parsed := urlParse $endpoint -}}
{{- $host := get $parsed "host" -}}
{{- $portWithColon := regexFind ":[0-9]+$" $host -}}
{{- if $portWithColon -}}
{{- trimPrefix ":" $portWithColon -}}
{{- else if eq (get $parsed "scheme") "https" -}}
443
{{- else if eq (get $parsed "scheme") "http" -}}
80
{{- else -}}
{{- fail "config.tracing.otlpEndpoint must use http or https" -}}
{{- end -}}
{{- end }}

{{- define "gophprofile.tracingEgressRule" -}}
- ports:
    - protocol: TCP
      port: {{ include "gophprofile.tracingEgressPort" . }}
{{- with .Values.networkPolicy.tracingEgress.to }}
  to:
{{ toYaml . | nindent 4 }}
{{- end }}
{{- end }}
