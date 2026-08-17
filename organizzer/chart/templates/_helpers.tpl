{{- define "organizzer.labels" -}}
app.kubernetes.io/name: organizzer
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "organizzer.selectorLabels" -}}
app.kubernetes.io/name: organizzer
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
The environment every worker shares, whichever board or stage it drives.
Per-board values (PROJECT_NUMBER, ALLOWED_REPOS) and per-stage values (the
model, the stage name itself) are set alongside this in the Deployment.
*/}}
{{- define "organizzer.env" -}}
- name: GITHUB_ORG
  value: {{ .Values.github.org | quote }}
- name: POLL_BUSY_SECONDS
  value: {{ .Values.poll.busySeconds | quote }}
- name: POLL_IDLE_SECONDS
  value: {{ .Values.poll.idleSeconds | quote }}
- name: BERGET_BASE_URL
  value: {{ .Values.llm.baseURL | quote }}
- name: BERGET_TIMEOUT_SECONDS
  value: {{ .Values.llm.timeoutSeconds | quote }}
- name: LLM_TEMPERATURE
  value: {{ .Values.llm.temperature | quote }}
- name: LLM_MAX_ATTEMPTS
  value: {{ .Values.llm.maxAttempts | quote }}
- name: MAX_ISSUES_PER_HOUR
  value: {{ .Values.llm.maxIssuesPerHour | quote }}
- name: KUBEFOUNDRY_WEBHOOK_URL
  value: {{ .Values.foundry.webhookURL | quote }}
- name: KUBEFOUNDRY_WEBHOOK_PATH
  value: {{ .Values.foundry.webhookPath | quote }}
- name: FOUNDRY_AGENT
  value: {{ .Values.foundry.agent | quote }}
- name: FOUNDRY_SKILLS
  value: {{ .Values.foundry.skills | quote }}
- name: FOUNDRY_SECRET_REF
  value: {{ .Values.foundry.secretRef | quote }}
- name: FOUNDRY_BRANCH
  value: {{ .Values.foundry.branch | quote }}
- name: FOUNDRY_MAX_RETRIES
  value: {{ .Values.foundry.maxRetries | quote }}
- name: FOUNDRY_GIT_AUTHOR_NAME
  value: {{ .Values.foundry.gitAuthorName | quote }}
- name: FOUNDRY_GIT_AUTHOR_EMAIL
  value: {{ .Values.foundry.gitAuthorEmail | quote }}
- name: LOG_LEVEL
  value: {{ .Values.logLevel | quote }}
{{- end -}}
