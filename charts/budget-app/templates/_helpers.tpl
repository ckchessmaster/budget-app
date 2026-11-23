{{- define "budget-app.fullname" -}}
{{- if .Release.Name -}}
{{- printf "%s-budget-app" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
budget-app
{{- end -}}
{{- end -}}

{{- define "budget-app.labels" -}}
app.kubernetes.io/part-of: budget-app
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "budget-app.secretName" -}}
{{- if .Values.database.existingSecret -}}
{{- .Values.database.existingSecret -}}
{{- else -}}
{{- printf "%s-db" (include "budget-app.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Browser origins allowed to call the API from another site.

Set backend.corsAllowedOrigins to control the list explicitly. When it is empty the
list is derived from the ingress hosts, because that is the only origin a real
browser can load the app from. With no ingress and no explicit list, no cross-site
access is granted at all.
*/}}
{{- define "budget-app.corsAllowedOrigins" -}}
{{- $origins := list -}}
{{- if .Values.backend.corsAllowedOrigins -}}
{{- range .Values.backend.corsAllowedOrigins -}}
{{- $origins = append $origins (printf "%s" . | trimSuffix "/") -}}
{{- end -}}
{{- else if and .Values.ingress.enabled .Values.ingress.hosts -}}
{{- range .Values.ingress.hosts -}}
{{- if .host -}}
{{- $origins = append $origins (printf "https://%s" .host) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- join "," $origins -}}
{{- end -}}
