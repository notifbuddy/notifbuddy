# notifbuddy

Slack channels for your Linear issues, self-hosted on Kubernetes.

The chart deploys the dashboard, the API, the auth service, and — for
evaluation — a PostgreSQL.

## Install

```console
helm install notifbuddy oci://ghcr.io/notifbuddy/charts/notifbuddy \
  --set domain=example.com \
  --set github.clientId=<id> \
  --set github.clientSecret=<secret>
```

Then point `app.example.com`, `api.example.com` and `auth.example.com` at your
ingress controller.

## Before you start

**A GitHub OAuth app.** It is the only sign-in method. Create one at
<https://github.com/settings/developers> with the callback URL
`https://auth.<domain>/api/auth/callback/github`.

**Three hostnames under one parent domain.** The session cookie is issued on
the parent so the dashboard and API can both see it. The chart refuses to
install if the hostnames share no common parent.

**An ingress controller**, and something to issue certificates — point
cert-manager at your issuer through `ingress.annotations`.

## Values

| Key | Default | Description |
| --- | --- | --- |
| `domain` | `""` | Parent domain. Required unless every `hosts` entry is set. |
| `hosts.dashboard` | `app.<domain>` | Dashboard hostname. |
| `hosts.api` | `api.<domain>` | API hostname. |
| `hosts.auth` | `auth.<domain>` | Auth service hostname. |
| `github.clientId` | `""` | GitHub OAuth client ID. Required. |
| `github.clientSecret` | `""` | GitHub OAuth client secret. Required unless `existingSecret` is set. |
| `existingSecret` | `""` | Secret you manage holding every sensitive value. **Required for GitOps** — see below. |
| `postgresql.enabled` | `true` | Bundled PostgreSQL. Evaluation only. |
| `database.url` | `""` | External application database. Setting it disables the bundled one. |
| `database.authUrl` | `""` | External auth database. |
| `ingress.enabled` | `true` | Create an Ingress for the three hostnames. |
| `ingress.className` | `""` | IngressClass, e.g. `nginx`. |
| `ingress.annotations` | `{}` | Ingress annotations, e.g. a cert-manager issuer. |
| `integrations.slack.*` | disabled | Slack app credentials. |
| `integrations.linear.*` | disabled | Linear app credentials. |
| `integrations.cloudflare.*` | disabled | Workers AI, for parsing @mentions into intents. |
| `integrations.email.*` | disabled | Resend, for organisation invitation emails. |

See [values.yaml](values.yaml) for the full set.

## Deploying with ArgoCD or Flux

**Set `existingSecret`.** Left unset, the chart generates its own secrets on
first install and reads them back from the cluster on upgrade. A GitOps
renderer runs `helm template` with no cluster access, so it cannot read them
back and generates fresh values on every sync — silently rotating the session
key and the token-encryption key. Everyone gets logged out and stored
integration tokens stop decrypting.

The Secret must carry `BETTER_AUTH_SECRET`, `INTEGRATION_ENC_KEY`,
`GITHUB_CLIENT_SECRET`, `DATABASE_URL`, `AUTHD_DATABASE_URL`, plus a key for
each integration you enable.

## The bundled PostgreSQL

On by default so a trial is one command. It is **evaluation only**: a single
replica, no backups, no failover, and no supported major-version upgrade path
— bumping the image tag over an existing volume is a data-loss trap. It also
runs as root, so clusters enforcing the restricted Pod Security Standard will
reject it.

For anything you intend to keep, run PostgreSQL yourself — [CloudNativePG] is
the usual answer on Kubernetes — and set `database.url` and `database.authUrl`.
Setting them disables the bundled instance.

## Upgrading

```console
helm upgrade notifbuddy oci://ghcr.io/notifbuddy/charts/notifbuddy --reuse-values
```

Schema migrations run automatically: the API migrates at startup, and the auth
service migrates from an init container. Generated secrets are read back and
preserved.

[CloudNativePG]: https://cloudnative-pg.io
