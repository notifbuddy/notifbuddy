{{/* Standard name/label helpers. */}}

{{- define "notifbuddy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "notifbuddy.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "notifbuddy.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "notifbuddy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Per-component selector labels. Call with (dict "ctx" $ "component" "backend"). */}}
{{- define "notifbuddy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "notifbuddy.name" .ctx }}
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Full label set for a pod template: the common labels plus the component.
Kept separate from selectorLabels because the two overlap on name/instance,
and emitting both into one map is a duplicate-key error.
*/}}
{{- define "notifbuddy.podLabels" -}}
{{ include "notifbuddy.labels" .ctx }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Hostnames. Each defaults to a subdomain of .Values.domain; either the domain
or all three explicit hosts must be set.

Written as if/else rather than `default (required ...)`: template arguments are
evaluated eagerly, so the `required` in a default would fire even when the host
was set explicitly.
*/}}
{{- define "notifbuddy.host.dashboard" -}}
{{- if .Values.hosts.dashboard -}}
{{- .Values.hosts.dashboard -}}
{{- else -}}
{{- printf "app.%s" (required "notifbuddy: set `domain` (or all three entries under `hosts`)" .Values.domain) -}}
{{- end -}}
{{- end -}}

{{- define "notifbuddy.host.api" -}}
{{- if .Values.hosts.api -}}
{{- .Values.hosts.api -}}
{{- else -}}
{{- printf "api.%s" (required "notifbuddy: set `domain` (or all three entries under `hosts`)" .Values.domain) -}}
{{- end -}}
{{- end -}}

{{- define "notifbuddy.host.auth" -}}
{{- if .Values.hosts.auth -}}
{{- .Values.hosts.auth -}}
{{- else -}}
{{- printf "auth.%s" (required "notifbuddy: set `domain` (or all three entries under `hosts`)" .Values.domain) -}}
{{- end -}}
{{- end -}}

{{/*
Cookie domain: the longest suffix shared by all three hostnames.

The session cookie is issued by the auth service and has to be readable by the
dashboard and the API, so it is set on the common parent. If the three hosts
share nothing (or only a public suffix like "com"), there is no domain that
would work and the install is rejected rather than left half-broken.
*/}}
{{- define "notifbuddy.cookieDomain" -}}
{{- if .Values.domain -}}
{{- printf ".%s" .Values.domain -}}
{{- else -}}
  {{- $hosts := list (include "notifbuddy.host.dashboard" .) (include "notifbuddy.host.api" .) (include "notifbuddy.host.auth" .) -}}
  {{- $parts := splitList "." (first $hosts) -}}
  {{- $common := "" -}}
  {{- range $i := until (len $parts) -}}
    {{- $candidate := join "." (slice $parts $i) -}}
    {{- if not $common -}}
      {{- $ok := true -}}
      {{- range $h := $hosts -}}
        {{- if not (or (eq $h $candidate) (hasSuffix (printf ".%s" $candidate) $h)) -}}
          {{- $ok = false -}}
        {{- end -}}
      {{- end -}}
      {{- if and $ok (gt (len (splitList "." $candidate)) 1) -}}
        {{- $common = $candidate -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
  {{- if not $common -}}
    {{- fail (printf "notifbuddy: hosts %v share no common parent domain, so no session cookie can be valid for all three. Put them under one domain." $hosts) -}}
  {{- end -}}
  {{- printf ".%s" $common -}}
{{- end -}}
{{- end -}}

{{/* Name of the Secret holding every sensitive value. */}}
{{- define "notifbuddy.secretName" -}}
{{- .Values.existingSecret | default (include "notifbuddy.fullname" .) -}}
{{- end -}}

{{/* Image reference for a component. Call with (dict "ctx" $ "name" "backend"). */}}
{{- define "notifbuddy.image" -}}
{{- $tag := .ctx.Values.image.tag | default .ctx.Chart.AppVersion -}}
{{- printf "%s/%s:%s" .ctx.Values.image.registry .name $tag -}}
{{- end -}}

{{/* Whether the bundled Postgres should be deployed: on unless an external URL was given. */}}
{{- define "notifbuddy.bundledPostgres" -}}
{{- if and .Values.postgresql.enabled (not .Values.database.url) -}}true{{- end -}}
{{- end -}}

{{- define "notifbuddy.postgresHost" -}}
{{- printf "%s-postgres" (include "notifbuddy.fullname" .) -}}
{{- end -}}

{{/*
Sensitive values, resolved once and reused by every template that needs them.

Order: an existing Secret in the cluster wins (so upgrades keep what is already
there), then values, then generated. `lookup` returns nothing when the chart is
rendered without cluster access — which is why GitOps users must set
`existingSecret`; see the note in values.yaml.
*/}}
{{- define "notifbuddy.secretData" -}}
{{- $prior := (lookup "v1" "Secret" .Release.Namespace (include "notifbuddy.fullname" .)) -}}
{{- $priorData := dict -}}
{{- if $prior -}}
  {{- range $k, $v := $prior.data -}}
    {{- $_ := set $priorData $k (b64dec $v) -}}
  {{- end -}}
{{- end -}}
{{- $pgPassword := $priorData.POSTGRES_PASSWORD | default (randAlphaNum 32) -}}
{{- $out := dict
      "BETTER_AUTH_SECRET" ($priorData.BETTER_AUTH_SECRET | default (randAlphaNum 48))
      "INTEGRATION_ENC_KEY" ($priorData.INTEGRATION_ENC_KEY | default (randAlphaNum 32 | b64enc))
      "POSTGRES_PASSWORD" $pgPassword
      "GITHUB_CLIENT_SECRET" (.Values.github.clientSecret | default $priorData.GITHUB_CLIENT_SECRET | default "")
-}}
{{- if include "notifbuddy.bundledPostgres" . -}}
  {{- $host := printf "%s:5432" (include "notifbuddy.postgresHost" .) -}}
  {{- $_ := set $out "DATABASE_URL" (printf "postgres://notifbuddy:%s@%s/notifbuddy?sslmode=disable" $pgPassword $host) -}}
  {{- $_ := set $out "AUTHD_DATABASE_URL" (printf "postgres://notifbuddy:%s@%s/authd?sslmode=disable" $pgPassword $host) -}}
{{- else -}}
  {{- $_ := set $out "DATABASE_URL" (required "notifbuddy: set `database.url` when the bundled Postgres is disabled" .Values.database.url) -}}
  {{- $_ := set $out "AUTHD_DATABASE_URL" (required "notifbuddy: set `database.authUrl` — the auth service needs its own database" .Values.database.authUrl) -}}
{{- end -}}
{{- if .Values.integrations.slack.enabled -}}
  {{- $_ := set $out "SLACK_CLIENT_SECRET" (required "notifbuddy: integrations.slack.clientSecret is required when Slack is enabled" .Values.integrations.slack.clientSecret) -}}
  {{- $_ := set $out "SLACK_SIGNING_SECRET" (required "notifbuddy: integrations.slack.signingSecret is required when Slack is enabled — the webhook handler fails closed without it" .Values.integrations.slack.signingSecret) -}}
{{- end -}}
{{- if .Values.integrations.linear.enabled -}}
  {{- $_ := set $out "LINEAR_CLIENT_SECRET" (required "notifbuddy: integrations.linear.clientSecret is required when Linear is enabled" .Values.integrations.linear.clientSecret) -}}
  {{- $_ := set $out "LINEAR_WEBHOOK_SECRET" (required "notifbuddy: integrations.linear.webhookSecret is required when Linear is enabled — the webhook handler fails closed without it" .Values.integrations.linear.webhookSecret) -}}
{{- end -}}
{{- if .Values.integrations.cloudflare.enabled -}}
  {{- $_ := set $out "CF_API_TOKEN" (required "notifbuddy: integrations.cloudflare.apiToken is required when Cloudflare Workers AI is enabled" .Values.integrations.cloudflare.apiToken) -}}
{{- end -}}
{{- if .Values.integrations.email.enabled -}}
  {{- $_ := set $out "RESEND_API_KEY" (required "notifbuddy: integrations.email.resendApiKey is required when email is enabled" .Values.integrations.email.resendApiKey) -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}
