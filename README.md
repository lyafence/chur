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

## Is chur for me?

**Use chur if:**
- You want to deliver secrets through tmpfs — never on disk or in env vars.
- You want applications to be unaware of secret backends.
- You need a lightweight, Kubernetes-native way to get secrets into memory.

**Don't use chur if:**
- Kubernetes Secret volumes already satisfy your requirements.
- You need automatic secret rotation (restart the Pod today).
- You already use a CSI driver successfully.

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
# Expected output:
# hello
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
| `chur.io/provider` | Secret backend: `env`, `local`, `k8s`, or `keeper` | Yes |
| `chur.io/secret-ref` | Reference to the secret (env var, file name, k8s Secret name, or keeper path like `prod/db/password`) | Yes |
| `chur.io/secret-key` | Key within the Kubernetes Secret's data map (used by the `k8s` provider) | No |
| `chur.io/mount-path` | Path to mount the tmpfs volume (default: `/secrets`) | No |
| `chur.io/keeper-skip-verify` | Skip TLS verification when calling `chur-keeper` (dev only, `"1"` or `"true"`) | No |
| `chur.io/provider-env` | Extra `CHUR_*` env vars for chur-init, JSON format: `{"KEY":"VAL"}`. Only applies when provider is `keeper`. Ignored for other providers. | No |

> **Dry-run behavior:** During dry-run requests (e.g., `kubectl apply --dry-run=server`),
> the webhook returns `Allowed=true` without mutation patches. This allows the API
> server to validate webhook connectivity without actuating side effects.

## Providers

| Provider   | Backend                          |
|------------|----------------------------------|
| `env`      | Environment variables (dev)      |
| `local`    | Files on host (bare-metal)       |
| `k8s`      | Kubernetes Secrets               |
| `keeper`   | Remote gateway (filesystem/exec)  |

The `local` provider reads secret files from `CHUR_LOCAL_BASE_PATH`
(default `/etc/chur/secrets`). When the provider is `local`, the webhook
automatically mounts the base directory as a read-only `hostPath` volume into
the `chur-init` container. You only need to ensure the files exist on the node
at the expected path.

The secret is still delivered to the application container through the
in-memory `emptyDir` volume at `/secrets` (or the path specified by
`chur.io/mount-path`).

## Optional: chur-keeper

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
`CHUR_KEEPER_URL` into chur-init containers of pods using the keeper provider,
when `CHUR_KEEPER_SERVICE_NAME` is configured (set automatically by the Helm chart
when keeper is enabled). Additional provider config can
be supplied through annotations:

| Annotation | Effect |
|---|---|
| `chur.io/keeper-skip-verify: "1"` or `"true"` | Injects `CHUR_KEEPER_SKIP_VERIFY=1` (dev only) |
| `chur.io/provider-env: '{"CHUR_KEEPER_SERVER_CA":"/etc/chur-keeper/ca.crt"}'` | Injects arbitrary `CHUR_*` env vars into `chur-init` |

For production mTLS, deploy keeper with `keeper.mtls.enabled=true`. The webhook
automatically injects `CHUR_KEEPER_TLS_CERT_PATH`, `CHUR_KEEPER_TLS_KEY_PATH`,
and `CHUR_KEEPER_SERVER_CA` into chur-init — no `chur.io/provider-env` annotation
needed. When `keeper.mtls.clientCert.secretName` is set, the webhook also mounts
the client certificate secret into the init container.

The `exec` backend runs a single executable with the ref as its argument.
The official keeper image is based on distroless and contains only the
chur-keeper binary. The executable referenced by `CHUR_KEEPER_EXEC_COMMAND`
must already exist in the container image. Shell scripts and third-party CLI tools require a custom keeper image.

The Helm chart exposes `keeper.extraInitContainers`, `keeper.extraVolumes`, and `keeper.extraVolumeMounts`, allowing executables to be prepared before chur-keeper starts. See the [Helm chart README](charts/chur/README.md) for deployment examples.

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
| `CHUR_INIT_IMAGE_PULL_POLICY` | `IfNotPresent` | webhook | Init container image pull policy |
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
| `CHUR_KEEPER_TLS_CERT_PATH` | — | webhook | Client TLS cert path for chur-init (keeper mTLS, auto-injected) |
| `CHUR_KEEPER_TLS_KEY_PATH` | — | webhook | Client TLS key path for chur-init (keeper mTLS, auto-injected) |
| `CHUR_KEEPER_SERVER_CA` | — | webhook | Server CA path for verifying keeper (keeper mTLS, auto-injected) |
| `CHUR_KEEPER_CLIENT_CERT_SECRET_NAME` | — | webhook | Kubernetes Secret name with client cert for chur-init (auto-mounted) |

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
