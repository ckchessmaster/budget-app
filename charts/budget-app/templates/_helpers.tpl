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
