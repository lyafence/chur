# chur

> **Status:** Pre-release. API may change without notice. Not production-ready.

![Status](https://img.shields.io/badge/status-pre--release-red)

Universal Multi-Cloud & Bare-Metal Zero-Trust Secret Injector for Kubernetes.

## Overview

chur is a Kubernetes admission webhook that intercepts Pod creation and
injects secrets directly into container memory (tmpfs), bypassing etcd, disk,
and environment variables. Secrets are sourced from any backend via a pluggable
provider architecture — AWS Secrets Manager, GCP Secret Manager, Azure Key Vault,
HashiCorp Vault, local files, Kubernetes Secrets, or environment variables.

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

See `.env.example` for all supported environment variables.

## License

MIT — see [LICENSE](./LICENSE) for details.
