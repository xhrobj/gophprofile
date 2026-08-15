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
        "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE dirty = false);" \
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
