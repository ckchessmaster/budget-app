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
Secret holding VAPID credentials when vapid.enabled is true.
*/}}
{{- define "budget-app.vapidSecretName" -}}
{{- if trim (.Values.vapid.existingSecret | default "") -}}
{{- trim .Values.vapid.existingSecret -}}
{{- else -}}
{{- printf "%s-vapid" (include "budget-app.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Secret and key holding DATABASE_URL for the backup CronJob.
Defaults to the chart's database Secret when backup.database.existingSecret is empty.
*/}}
{{- define "budget-app.backupDatabaseSecretName" -}}
{{- if trim (.Values.backup.database.existingSecret | default "") -}}
{{- trim .Values.backup.database.existingSecret -}}
{{- else -}}
{{- include "budget-app.secretName" . -}}
{{- end -}}
{{- end -}}

{{- define "budget-app.backupDatabaseSecretKey" -}}
{{- if trim (.Values.backup.database.existingSecret | default "") -}}
{{- .Values.backup.database.secretKey -}}
{{- else -}}
{{- .Values.database.secretKey -}}
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
{{- if .Values.vapid.enabled }}
{{- if not (trim (.Values.vapid.publicKey | default "")) }}
- name: VAPID_PUBLIC_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.vapidSecretName" . }}
      key: {{ .Values.vapid.publicKeySecretKey }}
{{- end }}
- name: VAPID_PRIVATE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.vapidSecretName" . }}
      key: {{ .Values.vapid.privateKeySecretKey }}
{{- if not (trim (.Values.vapid.subject | default "")) }}
- name: VAPID_SUBJECT
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.vapidSecretName" . }}
      key: {{ .Values.vapid.subjectSecretKey }}
{{- end }}
{{- end }}
{{- end -}}
