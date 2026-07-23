# chur — Agent Instructions

## Commands

| Command | Description |
|---------|-------------|
| `make build` | Build all binaries (webhook + init + keeper) |
| `make build-keeper` | Build chur-keeper only |
| `make build-init-minimal` | Build chur-init without k8s provider |
| `make build-init` | Build chur-init only |
| `make build-webhook` | Build chur-webhook only |
| `make fmt` | Format Go sources |
| `make lint` | Run golangci-lint |
| `make test` | Run tests |
| `make check` | Full verification: lint → test → build |
| `make vuln` | Run govulncheck |
| `make clean` | Remove build artifacts |
| `make docker` | Build all Docker images |
| `make docker-webhook` | Build webhook image |
| `make docker-init` | Build init image |
| `make docker-keeper` | Build keeper image |
| `make e2e` | End-to-end tests (Kind → deploy → verify). Set `E2E_SKIP_CLEANUP=true` to keep the Kind cluster. |
| `make release` | Build native binary tarball (local/CI quick release) |
| `make helm-package` | Package the Helm chart into `dist/` |

## Package Map

| Directory | Responsibility |
|-----------|---------------|
| `cmd/webhook/` | Webhook entrypoint (HTTP server, TLS) |
| `cmd/init/` | Init container entrypoint (secret fetching) |
| `cmd/keeper/` | Keeper entrypoint (HTTP server, TLS, backend dispatch) |
| `internal/webhook/` | Admission review handling, pod mutation |
| `internal/tls/` | TLS certificate generation and server config |
| `internal/provider/` | SecretProvider interface + Factory registry |
| `internal/providers/env/` | Environment variable provider |
| `internal/providers/local/` | Local file provider (bare-metal) |
| `internal/providers/k8s/` | Kubernetes Secret provider |
| `internal/metrics/` | Prometheus metric declarations and custom registry |
| `internal/validate/` | Input validation (filename-safe refs, secret keys) |
| `internal/keeper/` | Keeper server, config, backend interface |
| `internal/keeper/filesystem/` | Filesystem backend for chur-keeper |
| `internal/keeper/exec/` | Exec backend for chur-keeper |
| `internal/keeper/http/` | HTTP backend for chur-keeper |
| `internal/bytesize/` | Byte-size parsing utility |
| `internal/health/` | Shared health check HTTP handler (webhook + keeper) |
| `internal/providers/keeper/` | Keeper HTTP provider for chur-init |
| `test/e2e/` | End-to-end tests |
| `charts/chur/` | Helm chart for deploying the webhook |

## Architecture

```
Pod create request
       │
       ▼
chur-webhook (MutatingWebhookConfiguration)
       │
       ├── Parse annotations (chur.io/provider, chur.io/secret-ref)
       ├── Add emptyDir volume (medium: Memory)
       ├── Add chur-init init container
       └── Mount tmpfs to all containers
       
Pod starts
       │
       ▼
chur-init runs first
       │
       ├── Read CHUR_PROVIDER from env
       ├── Factory.Get(provider) → lazy init
       ├── SecretProvider.GetSecret(ref) → []byte
       │      │
       │      └── (if keeper provider) ──► chur-keeper (optional)
       │                                    │
       │                                    ├── filesystem backend
       │                                    ├── http backend (GET → upstream HTTPS API)
       │                                    └── exec backend (binary must exist in image)
       └── Write to tmpfs mount
       
App container runs
       │
       ▼
Read secret from /secrets/<ref> (tmpfs)
```

## Design Invariants

- Secrets never touch disk — tmpfs only (medium: Memory)
- Secrets never appear in env vars of app container
- Provider selection is annotation-driven, zero code changes
- Factory pattern with lazy initialization. Optional providers are compiled only
  when corresponding build tags are enabled (the `keeper` provider is the
  exception — it is stdlib-only and always included).
- All providers implement the same `SecretProvider` interface

## Design Philosophy

chur intentionally favors simplicity over features. Every new feature must justify
its weight.

When proposing changes, prefer:

- smaller binaries
- fewer dependencies
- stateless components
- Kubernetes-native behavior
- explicit configuration
- minimal runtime overhead

Avoid introducing additional control-plane components, CRDs, controllers, or
long-running background services unless they solve a demonstrated user problem.

## Coding Conventions

- Errors are always checked and wrapped with context.
- No global state (provider registry is the only exception, documented).
- Tests: table-driven, parallel-safe.
- `.env.example` documents all required environment variables and keeps default
  values consistent with the code and README.
- `.editorconfig` enforces consistent formatting.
- Every network, I/O, and OS boundary sets explicit timeouts,
  payload size limits, and connection caps.

## Agent Constraints

- **NEVER commit or push** without explicit command.
- Destructive git operations: ask first.

## Anti-Patterns

- **YAGNI** — don't add providers "just in case". Add when needed.
- **Global state creep** — the provider registry is justified; any other global state needs documentation.
- **SDK bloat** — don't add cloud SDK dependencies to Go code. The `k8s` provider is
  the only exception (requires `client-go`). Cloud secret stores (AWS, GCP, Azure,
   Vault) are covered by the keeper's `exec` or `http` backend — the executable
   must already exist in the keeper container image (distroless), or the
   secret server must expose an HTTPS API.
  Static Go binaries (e.g. `vault`) work via `extraInitContainers`.
  Python-based CLIs (`aws`, `gcloud`, `az`) require a Python runtime —
  use a custom image or External Secrets Operator.
  A secrets manager API needs only one operation — `GetSecret` — which does not
  justify pulling in an entire cloud SDK.
- **HTTP backend is not a cloud replacement** — `http` covers simple Bearer-auth
  HTTPS APIs (internal services, custom proxies). For AWS, Vault, GCP, or Azure,
  use `exec` with the respective CLI tool, or deploy a thin proxy that translates
  cloud-specific protocols to `GET ?ref=` + Bearer.
- **Keeper platform creep** — keeper is a thin stateless gateway, not a platform.
  No cache, no auth, no rotation. Every new feature must justify its weight
  against the principle: "keeper fetches secrets and returns them — nothing more."

## Current State

### Phase 1 ✅ — Core MVP (implemented)

**Providers:**
- `env`, `local`, `k8s`, `keeper` — all implemented, unit-tested, e2e-tested.
- Exponential backoff retry in `cmd/init/main.go`.
- Providers are registered via `init()` pattern; keeper is stdlib-only and always included.

**Webhook:**
- `MutatePod` parses `chur.io/*` annotations, injects tmpfs volume + chur-init init container + volume mounts.
- Idempotent: guards against `reinvocationPolicy: IfNeeded` duplicates.
- TLS: `server` and `mtls` modes, self-signed cert generation for dev.

**chur-keeper (optional):**
- Binary: `cmd/keeper/`, 9.6 MB. stdlib-only except prometheus client for `/metrics`.
- Backends: `filesystem`, `http`, and `exec` via `Backend` interface.
- Providers: `internal/providers/keeper/` — HTTP client with mTLS.
- Helm chart: `keeper.enabled=false`, conditional env injection.
- E2E: `TestE2E_KeeperProvider` in `test/e2e/`.

**CI/CD:**
- GitHub Actions: `ci.yml` (lint → test → build + vuln), `release.yml` (Docker multi-arch + Helm chart).
- Dependabot: gomod, docker, actions.

## Design Decisions

**Cloud SDKs are NOT compiled into chur.**  
`GetSecret` is one HTTP call — not worth pulling in cloud SDKs or Python CLI runtimes.  
Static Go binaries (Vault, custom helpers) work with the distroless image via `extraInitContainers`.  
Python-based CLIs (aws, gcloud, az) require a Python runtime — use a custom image or External Secrets Operator.

**HTTP backend is the simple path; exec is the escape hatch.**  
The `http` backend handles the simplest integration — a `GET` to an HTTPS API
with Bearer auth (internal services, custom secret proxies). For everything else —
Vault's `X-Vault-Token`, AWS SigV4, POST-based protocols, multi-step flows —
`exec` remains the universal escape hatch. `http` is not a cloud SDK replacement:
major cloud secret stores still require `exec` or a thin proxy. Together, `http`
and `exec` cover the full spectrum without pulling in a single cloud dependency.

## Future Ideas

Only if demonstrated demand:

- Sidecar hot-reload (inotify + polling).

## Architecture Overview

```
Phase 1 ✅
┌──────────────────────────────────┐
│  env                             │
│  local                           │
│  k8s                             │
│  keeper                          │
│  webhook                         │
│  Prometheus                      │
│  audit logging                   │
│  cloud secret stores             │
│  (via exec or http backend)      │
└──────────────────────────────────┘

## Release Workflow

Before preparing a release:

- Update README if behavior changed.
- Update THREAT_MODEL.md if security assumptions changed.
- Run:
  - `make check`
  - `make vuln`
  - `make e2e`

Push a version tag (e.g. `v<major>.<minor>.<patch>`) to trigger the GitHub Actions release workflow.
```
