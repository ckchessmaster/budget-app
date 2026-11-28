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

{{/*
Demo SimpleFIN bridge addressing.

The bridge is served by the API process. While the sync worker shared the API pod it
could reach it over loopback; as a separate Deployment it cannot, so the bridge binds a
routable address and is reached through its own Service. Both values are overridable for
installations that keep the worker co-located or run the bridge elsewhere.
*/}}
{{- define "budget-app.bridgePort" -}}
{{- int (.Values.backend.mockSimplefinPort | default 8090) -}}
{{- end -}}

{{- define "budget-app.bridgeListenAddr" -}}
{{- if .Values.backend.mockSimplefinListenAddr -}}
{{- .Values.backend.mockSimplefinListenAddr -}}
{{- else if .Values.temporal.worker.enabled -}}
0.0.0.0:{{ include "budget-app.bridgePort" . }}
{{- else -}}
127.0.0.1:{{ include "budget-app.bridgePort" . }}
{{- end -}}
{{- end -}}

{{- define "budget-app.bridgeBaseUrl" -}}
{{- if .Values.backend.mockSimplefinBaseUrl -}}
{{- .Values.backend.mockSimplefinBaseUrl -}}
{{- else if .Values.temporal.worker.enabled -}}
http://{{ include "budget-app.fullname" . }}-backend-bridge:{{ include "budget-app.bridgePort" . }}
{{- else -}}
http://127.0.0.1:{{ include "budget-app.bridgePort" . }}
{{- end -}}
{{- end -}}

{{/*
Opt-in transactional email configuration for household invites (sync worker).
*/}}
{{- define "budget-app.emailSecretName" -}}
{{- if trim (.Values.email.existingSecret | default "") -}}
{{- trim .Values.email.existingSecret -}}
{{- else -}}
{{- printf "%s-email" (include "budget-app.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "budget-app.emailPublicUrl" -}}
{{- if trim (.Values.email.publicUrl | default "") -}}
{{- trim .Values.email.publicUrl | trimSuffix "/" -}}
{{- else if and .Values.ingress.enabled .Values.ingress.hosts -}}
{{- $first := index .Values.ingress.hosts 0 -}}
{{- if $first.host -}}
{{- printf "https://%s" $first.host -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "budget-app.emailEnv" -}}
{{- if .Values.email.enabled }}
{{- if eq (trim (.Values.email.provider | default "resend")) "resend" }}
- name: RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.emailSecretName" . }}
      key: {{ .Values.email.apiKeySecretKey | default "RESEND_API_KEY" }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Opt-in identity provider user provisioning configuration (API backend).
*/}}
{{- define "budget-app.idpSecretName" -}}
{{- if trim (.Values.oidc.provisioning.existingSecret | default "") -}}
{{- trim .Values.oidc.provisioning.existingSecret -}}
{{- else -}}
{{- printf "%s-idp" (include "budget-app.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "budget-app.idpEnv" -}}
{{- if .Values.oidc.provisioning.enabled }}
{{- if eq (trim (.Values.oidc.provisioning.provider | default "authentik")) "authentik" }}
- name: AUTHENTIK_API_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "budget-app.idpSecretName" . }}
      key: {{ .Values.oidc.provisioning.apiTokenSecretKey | default "AUTHENTIK_API_TOKEN" }}
{{- end }}
{{- end }}
{{- end -}}
