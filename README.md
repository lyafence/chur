# chur

> **Status:** Alpha (stabilization in progress). API may change without notice.

![Status](https://img.shields.io/badge/status-alpha-yellow)

**Lightweight in-memory secret injection for Kubernetes.**

chur is not another secrets manager. It is the simplest secure way to deliver a
secret directly into the memory of a Kubernetes workload. See
[THREAT_MODEL.md](THREAT_MODEL.md) for the security model.

## Overview

chur is a Kubernetes admission webhook that intercepts Pod creation and
injects secrets directly into container memory (tmpfs), bypassing application
environment variables and Kubernetes Secret volumes. Secrets are sourced from
environment variables, local files on the node, or Kubernetes Secrets via a
pluggable provider architecture. Additional cloud providers (AWS, GCP, Azure,
Vault) are planned for future releases.

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

## Why chur?

| chur | Kubernetes Secret volume |
|------|--------------------------|
| In-memory delivery | Secret volume |
| No application env vars | Env vars optional |
| Admission-based injection | Native volume mount |
| Lightweight | Kubernetes built-in |

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

## Prerequisites

- Kubernetes cluster 1.28+
- `helm` 3.x
- `kubectl` with access to the cluster

## Quick Start

```bash
# Add the repository and install the webhook
helm repo add chur https://lyafence.github.io/chur
helm repo update
helm install chur chur/chur --namespace chur-system --create-namespace --wait

# Deploy a test Pod and verify injection
kubectl create secret generic my-secret --from-literal=token=hello
kubectl run test-pod --image=busybox --restart=Never \
  --annotations=chur.io/provider=k8s \
  --annotations=chur.io/secret-ref=my-secret \
  --annotations=chur.io/secret-key=token \
  --serviceaccount=chur-init \
  --command -- sleep 9999
kubectl exec test-pod -- cat /secrets/my-secret
```

The default TLS mode uses cert-manager. For development without cert-manager
(`tls.provider=helmGenerated`) or other TLS options, see
[`charts/chur/values.yaml`](charts/chur/values.yaml) and the
[Helm chart README](charts/chur/README.md).

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
  serviceAccountName: chur-init
  containers:
    - name: app
      image: my-app:latest
```

chur-webhook intercepts the Pod, injects an `emptyDir` with `medium: Memory`
and a `chur-init` init container that fetches the secret and writes it to tmpfs.
The application reads the secret from `/secrets/<ref>` (e.g. `/secrets/db-credentials`).

> **Note:** Secrets are fetched once at Pod startup. To rotate secrets, restart
> the Pod (e.g. via `kubectl rollout restart`). Hot-reload may be considered in
> later versions.

### Annotations

| Annotation | Description | Required |
|------------|-------------|----------|
| `chur.io/provider` | Secret backend: `env`, `local`, or `k8s` | Yes |
| `chur.io/secret-ref` | Reference to the secret (env var, file name, or k8s Secret name) | Yes |
| `chur.io/secret-key` | Extract a specific key from a JSON secret value | No |
| `chur.io/mount-path` | Path to mount the tmpfs volume (default: `/secrets`) | No |

## Providers

| Provider   | Backend                          | Phase |
|------------|----------------------------------|-------|
| `env`      | Environment variables (dev)      | 1 ✅  |
| `local`    | Files on host (bare-metal)       | 1 ✅  |
| `k8s`      | Kubernetes Secrets               | 1 ✅  |
| `aws`      | AWS Secrets Manager              | 2 🚧  |
| `gcp`      | GCP Secret Manager               | 2 🚧  |
| `azure`    | Azure Key Vault                  | 2 🚧  |
| `vault`    | HashiCorp Vault                  | 2 🚧  |

_Phase 1 providers are implemented and tested. Phase 2 providers are planned._

## chur-keeper (optional)

`chur-keeper` is an optional standalone HTTPS gateway that exposes secrets to
`chur-init` via a single endpoint:

- `POST /v1/secrets/get` — accepts `{"ref":"..."}`, returns raw secret bytes.

Backends are selected via `CHUR_KEEPER_BACKEND`:

| Backend | Variable | Description |
|---------|----------|-------------|
| `filesystem` | `CHUR_KEEPER_BACKEND=filesystem` | Reads secrets from files under `CHUR_KEEPER_BACKEND_FS_ROOT` |
| `exec` | `CHUR_KEEPER_BACKEND=exec` | Executes `CHUR_KEEPER_EXEC_COMMAND ref` |

To use it, annotate a Pod with `chur.io/provider: keeper` and set
`chur.io/secret-ref` to the keeper ref. The webhook automatically injects
`CHUR_KEEPER_URL` when keeper is enabled in Helm. Additional provider config can
be supplied through annotations:

| Annotation | Effect |
|---|---|
| `chur.io/keeper-skip-verify: "true"` | Injects `CHUR_KEEPER_SKIP_VERIFY=1` (dev only) |
| `chur.io/provider-env: '{"CHUR_KEEPER_SERVER_CA":"/etc/chur-keeper/ca.crt"}'` | Injects arbitrary `CHUR_*` env vars into `chur-init` |

In production, deploy keeper with mTLS and use `chur.io/provider-env` to point
`chur-init` at mounted client certificates.

### Local provider in Kubernetes

The `local` provider reads secret files from `CHUR_LOCAL_BASE_PATH`
(default `/etc/chur/secrets`). When the provider is `local`, the webhook
automatically mounts the base directory as a read-only `hostPath` volume into
the `chur-init` container. You only need to ensure the files exist on the node
at the expected path.

The secret is still delivered to the application container through the
in-memory `emptyDir` volume at `/secrets` (or the path specified by
`chur.io/mount-path`).

## Configuration

Main environment variables. For all other variables (TLS auto-generation, mTLS,
init container configuration), see [`.env.example`](.env.example).

| Variable | Default | Description |
|----------|---------|-------------|
| `CHUR_LISTEN` | `:8443` | Webhook listen address (admission) |
| `CHUR_HEALTH_LISTEN` | `:8080` | Webhook listen address (health probes) |
| `CHUR_TLS_MODE` | `server` | TLS mode: `server` or `mtls` |
| `CHUR_VOLUME_SIZE_LIMIT` | `10Mi` | Max size of tmpfs volume per pod |
| `CHUR_ALLOWED_NAMESPACES` | (all) | Comma-separated allowlist of namespaces |
| `CHUR_INIT_IMAGE` | `ghcr.io/lyafence/chur-init:latest` | Init container image |
| `CHUR_PROVIDER` | `env` | Secret provider: `env`, `local`, `k8s` |
| `CHUR_MAX_SECRET_SIZE` | `1Mi` | Max secret size in init container |
| `CHUR_MAX_CONCURRENT` | `100` | Maximum concurrent admission review requests |

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
  name: chur-init
```

For production, consider restricting access to specific secrets via
`resourceNames` in the Role.

## License

MIT — see [LICENSE](LICENSE) for details.
