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

{{/*
Secret holding VAULT_MASTER_KEY. Defaults to the database Secret so a deployment keeps
one secret for both, which also guarantees the API and the sync worker resolve the same
key: they have to decrypt the same simplefin_connections rows.
*/}}
{{- define "budget-app.vaultSecretName" -}}
{{- if .Values.vault.existingSecret -}}
{{- .Values.vault.existingSecret -}}
{{- else if .Values.database.existingSecret -}}
{{- .Values.database.existingSecret -}}
{{- else -}}
{{- include "budget-app.secretName" . -}}
{{- end -}}
{{- end -}}

{{/*
Credentials shared by the API and the Temporal sync worker.
*/}}
{{- define "budget-app.syncEnv" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.secretName" . }}
      key: {{ .Values.database.secretKey }}
- name: VAULT_MASTER_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.vaultSecretName" . }}
      key: {{ .Values.vault.secretKey }}
{{- end -}}
