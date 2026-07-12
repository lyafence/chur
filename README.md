# chur

> **Status:** Beta. The core architecture is considered stable, but APIs,
> configuration, and operational details may still change before v1.0.
> Feedback and production-like testing are welcome.

![Status](https://img.shields.io/badge/status-beta-yellow)
![Go](https://img.shields.io/badge/go-1.26.4-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Release](https://img.shields.io/github/v/release/lyafence/chur)

**In-memory secret delivery for Kubernetes.**  

Kubernetes Secrets are a great storage mechanism, but they are not always the
best delivery mechanism — they end up in etcd, environment variables, disk
files, crash dumps, and debug logs. chur changes how secrets reach your
workload: it injects them directly into container memory (tmpfs) at Pod start
and never touches disk or environment variables. It is the simplest secure way
to deliver a secret into a Kubernetes workload — not another secrets manager.
See [THREAT_MODEL.md](THREAT_MODEL.md) for the security model.

## Overview

chur is a Kubernetes admission webhook that intercepts Pod creation and
injects secrets directly into container memory (tmpfs), bypassing application
environment variables and Kubernetes Secret volumes. Secrets are sourced from
environment variables, local files on the node, or Kubernetes Secrets via a
pluggable provider architecture. Cloud secret stores (AWS, GCP, Azure, Vault)
are covered by the optional `chur-keeper` gateway with its `exec` backend —
no Go SDK dependencies needed.

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
                          │ │    │      │ │
                          │ │    └──(keeper)──► chur-keeper (optional)
                          │ │                  ├── filesystem backend
                          │ │                  └── exec backend
                          │ └──────────┘ │
                          │ ┌──────────┐ │
                          │ │  app     │ │  ← reads secret from tmpfs file
                          │ └──────────┘ │
                          └──────────────┘
```

## Why chur?

| Feature | chur | Kubernetes Secret volume |
|---------|------|--------------------------|
| Delivery | In-memory tmpfs | File on disk |
| Env vars in app | Never injected | Optional |
| Injection | Admission webhook | Native volume mount |
| Overhead | Init container only | kubelet watcher |

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

- Kubernetes 1.28+ cluster
- `helm` 3.x
- `kubectl` with access to the cluster

## Quick Start

```bash
# Add the repository and install the webhook
helm repo add chur https://lyafence.github.io/chur
helm repo update

# Install into a dedicated namespace (RBAC resources are created here)
helm install chur chur/chur --namespace chur-system --create-namespace --wait

# For the optional chur-keeper, enable it at install time:
# helm install chur chur/chur --namespace chur-system --create-namespace --wait \
#   --set keeper.enabled=true

# Deploy a test Pod in the same namespace and verify injection
kubectl -n chur-system create secret generic my-secret --from-literal=token=hello
kubectl -n chur-system run test-pod --image=busybox --restart=Never \
  --annotations=chur.io/provider=k8s \
  --annotations=chur.io/secret-ref=my-secret \
  --annotations=chur.io/secret-key=token \
  --serviceaccount=chur-init \
  --command -- sleep 9999
kubectl -n chur-system exec test-pod -- cat /secrets/my-secret
```

The default TLS mode uses cert-manager. For development without cert-manager
(`tls.provider=helmGenerated`) or other TLS options, see
[`charts/chur/values.yaml`](charts/chur/values.yaml) and the
[Helm chart README](charts/chur/README.md).

To try the optional `chur-keeper` gateway (install with `--set keeper.enabled=true`):

```bash
# Deploy a test Pod with the keeper provider
kubectl -n chur-system run test-keeper --image=busybox --restart=Never \
  --annotations=chur.io/provider=keeper \
  --annotations=chur.io/secret-ref=prod/db/password \
  --annotations=chur.io/keeper-skip-verify=true \
  --command -- sleep 9999
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
| `chur.io/provider` | Secret backend: `env`, `local`, `k8s`, or `keeper` | Yes |
| `chur.io/secret-ref` | Reference to the secret (env var, file name, k8s Secret name, or keeper path like `prod/db/password`) | Yes |
| `chur.io/secret-key` | Key within the Kubernetes Secret's data map (used by the `k8s` provider) | No |
| `chur.io/mount-path` | Path to mount the tmpfs volume (default: `/secrets`) | No |
| `chur.io/keeper-skip-verify` | Skip TLS verification when calling `chur-keeper` (dev only, `"1"` or `"true"`) | No |
| `chur.io/provider-env` | Extra `CHUR_*` env vars for chur-init, JSON format: `{"KEY":"VAL"}` | No |

## Providers

| Provider   | Backend                          | Phase |
|------------|----------------------------------|-------|
| `env`      | Environment variables (dev)      | 1 ✅  |
| `local`    | Files on host (bare-metal)       | 1 ✅  |
| `k8s`      | Kubernetes Secrets               | 1 ✅  |
| `keeper`   | Remote gateway (filesystem/exec)  | 1 ✅  |

_Phase 1 providers are implemented and tested. Cloud secret stores (AWS, GCP, Azure,
Vault) are covered by chur-keeper's `exec` backend — no Go SDK dependencies needed._

## chur-keeper (optional)

`chur-keeper` is an optional stateless HTTPS gateway. It fetches secrets from a
configurable backend — it does not store them, cache them, or authenticate
callers. It exposes a single endpoint to `chur-init`:

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
| `chur.io/keeper-skip-verify: "1"` or `"true"` | Injects `CHUR_KEEPER_SKIP_VERIFY=1` (dev only) |
| `chur.io/provider-env: '{"CHUR_KEEPER_SERVER_CA":"/etc/chur-keeper/ca.crt"}'` | Injects arbitrary `CHUR_*` env vars into `chur-init` |

In production, deploy keeper with mTLS and use `chur.io/provider-env` to point
`chur-init` at mounted client certificates.

To integrate with cloud secret stores (AWS Secrets Manager, GCP Secret Manager,
Azure Key Vault, HashiCorp Vault), use the `exec` backend — chur-keeper runs
the specified CLI command with the ref as an argument. For example:

The `exec` backend runs a single command — `exec.Command` does not invoke a shell,
so `CHUR_KEEPER_EXEC_COMMAND` must be a single executable without shell syntax.

Create a wrapper script (e.g. `/usr/local/bin/get-aws-secret`):

```bash
#!/bin/sh
aws secretsmanager get-secret-value --secret-id "$1" --query SecretString --output text
```

Then configure:

```bash
CHUR_KEEPER_BACKEND=exec
CHUR_KEEPER_EXEC_COMMAND=/usr/local/bin/get-aws-secret
```

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

| Variable | Default | Scope | Description |
|----------|---------|-------|-------------|
| `CHUR_LISTEN` | `:8443` | webhook | Listen address (admission) |
| `CHUR_HEALTH_LISTEN` | `:8080` | webhook | Listen address (health probes) |
| `CHUR_TLS_MODE` | `server` | webhook | TLS mode: `server` or `mtls` |
| `CHUR_VOLUME_SIZE_LIMIT` | `10Mi` | webhook | Max size of tmpfs volume per pod |
| `CHUR_ALLOWED_NAMESPACES` | (all) | webhook | Comma-separated allowlist |
| `CHUR_INIT_IMAGE` | `ghcr.io/lyafence/chur-init:latest` | webhook | Init container image |
| `CHUR_MAX_CONCURRENT` | `100` | webhook | Max concurrent admission reviews |
| `CHUR_PROVIDER` | `env` | init | Secret provider name |
| `CHUR_MAX_SECRET_SIZE` | `1Mi` | init | Max secret size |
| `CHUR_KEEPER_URL` | `https://chur-keeper.chur-system.svc:9443` | init | Keeper service URL (auto-injected) |
| `CHUR_KEEPER_LISTEN` | `:9443` | keeper | HTTPS listen address |
| `CHUR_KEEPER_HEALTH_LISTEN` | `:9444` | keeper | Health endpoint listen address |
| `CHUR_KEEPER_TLS_MODE` | `self-signed` | keeper | TLS mode: `self-signed` or `mtls` |
| `CHUR_KEEPER_BACKEND` | `filesystem` | keeper | Backend type: `filesystem` or `exec` |
| `CHUR_KEEPER_MAX_SECRET_SIZE` | `1Mi` | keeper | Maximum response size |
| `CHUR_KEEPER_EXEC_COMMAND` | — | keeper | Command to execute (exec backend) |
| `CHUR_KEEPER_BACKEND_FS_ROOT` | `/var/lib/chur-keeper/secrets` | keeper | Root directory (filesystem backend) |

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
