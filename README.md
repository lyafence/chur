# chur

> **Status:** Alpha (stabilization in progress). API may change without notice.

![Status](https://img.shields.io/badge/status-alpha-yellow)

**Lightweight in-memory secret injection for Kubernetes.**

CHUR is not another secrets manager. It is the simplest secure way to deliver a
secret directly into the memory of a Kubernetes workload. See
[THREAT_MODEL.md](THREAT_MODEL.md) for the security model.

## Overview

chur is a Kubernetes admission webhook that intercepts Pod creation and
injects secrets directly into container memory (tmpfs), bypassing application
environment variables and Kubernetes Secret volumes. Secrets are sourced from
any backend via a pluggable provider architecture — AWS Secrets Manager, GCP
Secret Manager, Azure Key Vault, HashiCorp Vault, local files, Kubernetes
Secrets, or environment variables.

## Why CHUR?

| CHUR | Kubernetes Secret volume |
|------|--------------------------|
| In-memory delivery | Secret volume |
| No application env vars | Env vars optional |
| Admission-based injection | Native volume mount |
| Lightweight | Built-in |

## Security

- Secrets are delivered to a tmpfs (`emptyDir` with `medium: Memory`) and never
  written to disk.
- The secret value is never injected into the application container environment.
- `chur-init` runs as a non-root user with a read-only root filesystem and
  dropped capabilities.
- The secret file is written with group-readable permissions (`0640`) and shared
  via `fsGroup`.
- Unknown providers are rejected at admission time.
- Request size, concurrency, and secret size are bounded.

See [THREAT_MODEL.md](THREAT_MODEL.md) for the full threat model and
non-goals.

## Architecture

```
                         ┌──────────────┐
                         │  API Server   │
                         └──────┬───────┘
                                │ admission review
                         ┌──────▼───────┐
                         │ chur-webhook │  ← MutatingWebhookConfiguration
                         └──────┬───────┘
                                │ JSON patch: add tmpfs volume + init container
                         ┌──────▼───────┐
                         │    Pod        │
                         │ ┌──────────┐ │
                         │ │chur-init │ │  ← reads secret from provider, writes to tmpfs
                         │ └──────────┘ │
                         │ ┌──────────┐ │
                         │ │  app     │ │  ← reads secret from tmpfs file
                         │ └──────────┘ │
                         └──────────────┘
```

## Providers

| Provider   | Backend                          | Phase |
|------------|----------------------------------|-------|
| `env`      | Environment variables (dev)      | 1 ✅  |
| `local`    | Files on host (bare-metal)       | 1 ✅  |
| `k8s`      | Kubernetes Secrets (fallback)    | 1 ✅  |
| `aws`      | AWS Secrets Manager              | 2 🚧  |
| `gcp`      | GCP Secret Manager               | 2 🚧  |
| `azure`    | Azure Key Vault                  | 2 🚧  |
| `vault`    | HashiCorp Vault                  | 2 🚧  |

_Phase 1 providers are implemented and tested. Phase 2 providers are planned._

### Local provider in Kubernetes

The `local` provider reads secret files from `CHUR_LOCAL_BASE_PATH`
(default `/etc/chur/secrets`). When the provider is `local`, the webhook
automatically mounts the base directory as a read-only `hostPath` volume into
the `chur-init` container. You only need to ensure the files exist on the node
at the expected path.

The secret is still delivered to the application container through the
in-memory `emptyDir` volume at `/secrets` (or the path specified by
`chur.io/mount-path`).

## Quick Start

```bash
# Build both binaries
make build

# Run tests
make test

# Build Docker images
make docker
```

## Usage

Annotate your Pod with the secret source:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  annotations:
    chur.io/provider: "k8s"
    chur.io/secret-ref: "db-credentials"
    chur.io/secret-key: "password"   # optional: key within the k8s Secret
    chur.io/mount-path: "/secrets"
spec:
  containers:
    - name: app
      image: my-app:latest
```

chur-webhook intercepts the Pod, injects an `emptyDir` with `medium: Memory`
and a `chur-init` init container that fetches the secret and writes it to tmpfs.
The application reads the secret from `/secrets/<ref>` (e.g. `/secrets/db-credentials`).

> **Note:** In Phase 1, secrets are fetched once at Pod startup. To rotate
> secrets, restart the Pod (e.g. via `kubectl rollout restart`). Hot-reload
> is planned for Phase 3.

## Configuration

Main environment variables (see [`.env.example`](./.env.example) for the full list):

| Variable | Default | Description |
|----------|---------|-------------|
| `CHUR_LISTEN` | `:8443` | Webhook listen address (admission) |
| `CHUR_HEALTH_LISTEN` | `:8080` | Webhook listen address (health probes) |
| `CHUR_TLS_MODE` | `server` | TLS mode: `server` or `mtls` |
| `CHUR_TLS_AUTO_GENERATE` | — | Set to `1` to auto-generate self-signed TLS cert when not mounted (dev only) |
| `CHUR_TLS_CERT_DNS_NAME` | `localhost` | DNS name for auto-generated cert (only when `CHUR_TLS_AUTO_GENERATE=1`) |
| `CHUR_CLIENT_CA_PATH` | `/etc/chur/ca.crt` | Path to CA certificate for verifying API server client cert (mtls only) |
| `CHUR_VOLUME_SIZE_LIMIT` | `10Mi` | Max size of tmpfs volume per pod |
| `CHUR_ALLOWED_NAMESPACES` | (all) | Comma-separated allowlist of namespaces |
| `CHUR_INIT_IMAGE` | `ghcr.io/lyafence/chur-init:latest` | Init container image |
| `CHUR_PROVIDER` | `env` | Secret provider: `env`, `local`, `k8s` |
| `CHUR_MAX_SECRET_SIZE` | `1Mi` | Max secret size in init container |
| `CHUR_MAX_CONCURRENT` | `100` | Maximum concurrent admission review requests |

## Helm Installation

A Helm chart is provided under `charts/chur/`.

### Production (with cert-manager)

```bash
helm install chur ./charts/chur --wait
```

Requirements:
- Kubernetes >= 1.28
- [cert-manager](https://cert-manager.io) installed in the cluster

The chart creates a self-signed `Issuer` and a `Certificate` for the webhook's
TLS cert. The `caBundle` in the `MutatingWebhookConfiguration` is injected
automatically by cert-manager's `cainjector`.

### Development / CI (without cert-manager)

```bash
helm install chur ./charts/chur -f ./charts/chur/values-dev.yaml --wait
```

### User-provided certificate

```bash
helm install chur ./charts/chur \
  --set tls.provider=userSecret \
  --set tls.userSecret.name=my-tls-secret \
  --set tls.userSecret.caBundle="$(base64 -w0 < ca.crt)"
```

### mTLS

By default, the webhook only presents a TLS server certificate. To require the
API server to present a client certificate, set `webhook.tlsMode=mtls` and
provide the CA bundle that signed the API server's client certificate:

```bash
helm install chur ./charts/chur \
  --set webhook.tlsMode=mtls \
  --set-file mtls.caBundle=api-server-client-ca.crt
```

> ⚠️ mTLS mode is not available in most managed Kubernetes clusters (EKS, GKE,
> AKS) unless you have access to the API server's command-line flags.

### Using the Helm repository

```bash
helm repo add chur https://lyafence.github.io/chur
helm repo update
helm install chur chur/chur --wait
```

## RBAC Requirements

When using the `k8s` provider, the init container reads Secrets via the
Kubernetes API. The Pod's ServiceAccount must have a Role binding with
`get` access to Secrets in the target namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: chur-secret-reader
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: chur-secret-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: chur-secret-reader
subjects:
- kind: ServiceAccount
  name: default
```

For production, consider restricting access to specific secrets via
`resourceNames` in the Role.

## License

MIT — see [LICENSE](./LICENSE) for details.
