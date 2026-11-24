{{/* The chart's name, and a release name that does not repeat it. */}}
{{- define "kubelook.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubelook.fullname" -}}
{{- $name := include "kubelook.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kubelook.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubelook.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Labels of every object. */}}
{{- define "kubelook.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "kubelook.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* The image: a digest wins over a tag, and the tag defaults to the appVersion. */}}
{{- define "kubelook.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "kubelook.postgres" -}}
{{- eq .Values.database.driver "postgres" | ternary "true" "" -}}
{{- end -}}

{{/* The port of the container: the one in server.listenAddress. */}}
{{- define "kubelook.port" -}}
{{- $p := regexFind "[0-9]+$" (default ":8080" .Values.server.listenAddress) -}}
{{- default "8080" $p -}}
{{- end -}}

{{/* The directory of the SQLite file: the volume is mounted there. */}}
{{- define "kubelook.dataDir" -}}
{{- dir .Values.database.sqlitePath -}}
{{- end -}}

{{/*
config.yaml: the values the application knows. Chart-only keys (secret references, the cleanup schedule)
are removed; the application's loader refuses unknown keys.
*/}}
{{- define "kubelook.config" -}}
{{- $v := .Values -}}
{{- $c := dict -}}
{{- range $k := list "server" "session" "kubeconfig" "fileBrowser" "logs" "shell" "nodeShell" "debug" "helm" "watch" "memory" "maintenance" "modules" "prometheus" "telemetry" "egress" -}}
{{- if hasKey $v $k -}}{{- $_ := set $c $k (get $v $k) -}}{{- end -}}
{{- end -}}
{{- $_ := set $c "database" (pick $v.database "driver" "sqlitePath" "migrateOnStart") -}}
{{- $auth := dict -}}
{{- range $k, $val := $v.auth -}}
{{- if kindIs "map" $val -}}{{- $_ := set $auth $k (omit $val "clientSecretRef") -}}{{- else -}}{{- $_ := set $auth $k $val -}}{{- end -}}
{{- end -}}
{{- $_ := set $c "auth" $auth -}}
{{- $_ := set $c "vault" $v.vault -}}
{{- $_ := set $c "audit" (omit $v.audit "cleanup") -}}
{{- $_ := required "server.publicURL is required" $v.server.publicURL -}}
{{- toYaml $c -}}
{{- end -}}

{{/* Environment variables from Secrets that every Kubelook container needs to load its configuration. */}}
{{- define "kubelook.secretEnv" -}}
- name: KUBELOOK_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.encryption.keySecret }}
      key: key
{{- with .Values.auth.google.clientSecretRef }}
{{- if .name }}
- name: KUBELOOK_GOOGLE_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .name }}
      key: {{ .key }}
{{- end }}
{{- end }}
{{- with .Values.auth.github.clientSecretRef }}
{{- if .name }}
- name: KUBELOOK_GITHUB_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .name }}
      key: {{ .key }}
{{- end }}
{{- end }}
{{- if include "kubelook.postgres" . }}
- name: KUBELOOK_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.database.existingSecret }}
      key: url
{{- end }}
{{- end -}}

{{- define "kubelook.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "kubelook.containerSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65532
readOnlyRootFilesystem: true
allowPrivilegeEscalation: false
capabilities:
  drop: [ALL]
{{- end -}}

{{/* The container that runs `kubelook migrate`. */}}
{{- define "kubelook.migrateContainer" -}}
- name: migrate
  image: {{ include "kubelook.image" . | quote }}
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  args: [migrate]
  env:
    - name: KUBELOOK_MIGRATE_DATABASE_URL
      valueFrom:
        secretKeyRef:
          name: {{ .Values.database.migrationSecret }}
          key: url
    {{- include "kubelook.secretEnv" . | nindent 4 }}
  securityContext: {{- include "kubelook.containerSecurityContext" . | nindent 4 }}
  volumeMounts:
    - {name: config, mountPath: /etc/kubelook, readOnly: true}
    - {name: tmp, mountPath: /tmp}
{{- end -}}
