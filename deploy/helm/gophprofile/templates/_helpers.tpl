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
