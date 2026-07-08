# chur — Agent Instructions

## Commands

| Command | Description |
|---------|-------------|
| `make build` | Build both binaries (webhook + init) |
| `make build-webhook` | Build chur-webhook only |
| `make build-init` | Build chur-init only |
| `make fmt` | Format Go sources |
| `make lint` | Run golangci-lint |
| `make test` | Run tests |
| `make check` | Full verification: lint → test → build |
| `make vuln` | Run govulncheck |
| `make clean` | Remove build artifacts |
| `make docker` | Build all Docker images |
| `make docker-webhook` | Build webhook image |
| `make docker-init` | Build init image |
| `make e2e` | End-to-end tests (Kind → deploy → verify). Set `E2E_SKIP_CLEANUP=true` to keep the Kind cluster. |
| `make release` | Build native binary tarball (local/CI quick release) |
| `make helm-package` | Package the Helm chart into `dist/` |
| `goreleaser release` | Full cross-platform release + Docker images |

## Package Map

| Directory | Responsibility |
|-----------|---------------|
| `cmd/webhook/` | Webhook entrypoint (HTTP server, TLS) |
| `cmd/init/` | Init container entrypoint (secret fetching) |
| `internal/webhook/` | Admission review handling, pod mutation, TLS |
| `internal/provider/` | SecretProvider interface + Factory registry |
| `internal/providers/env/` | Environment variable provider |
| `internal/providers/local/` | Local file provider (bare-metal) |
| `internal/providers/k8s/` | Kubernetes Secret provider |
| `internal/validate/` | Input validation (filename-safe refs, secret keys) |
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
  when corresponding build tags are enabled.
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

## Agent Constraints

- **NEVER commit or push** without explicit command.
- Destructive git operations: ask first.

## Anti-Patterns

- **YAGNI** — don't add providers "just in case". Add when needed.
- **Global state creep** — the provider registry is justified; any other global state needs documentation.
- **SDK bloat** — prefer lightweight HTTP clients over full cloud SDKs where possible.
  The `k8s` provider is an exception (requires `client-go`). For cloud secret stores
  (AWS, GCP, Azure), prefer minimal HTTP implementations to keep the init binary
  small. A secrets manager API typically needs only one operation — `GetSecret` —
  which does not justify pulling in an entire cloud SDK.

## Roadmap

### Phase 1: Core MVP — Base Providers + Webhook

**1a: Provider Implementations + Unit Tests**
- `env`: GetSecret reads `os.Getenv`. Test: set env var → get value.
- `local`: GetSecret reads a file from disk. `CHUR_LOCAL_BASE_PATH` configures the
  base directory (default `/etc/chur/secrets`). Test: temp file → read → cleanup.
- `k8s`: GetSecret via InClusterConfig + client-go. Test: fake clientset (`k8s.io/client-go/testing`).
- Exponential backoff retry — network may not be ready immediately in init containers.

**1b: Webhook — Mutation Logic**
- Full `MutatePod` implementation: parse annotations → JSON Patch (tmpfs + init container + mount).
- TLS: self-signed cert for dev mode.
- Test: unit-test JSON patches on raw Pod manifests (no K8s API needed).

**1c: End-to-End with Kind**
- `test/e2e/e2e_test.go`: Kind cluster → deploy webhook → deploy annotated Pod → verify secret in tmpfs.
- Make target: `make e2e` (Kind up → test → cleanup).

### Phase 2: Cloud Providers — AWS + GCP + Azure

**2a: AWS Provider** (`internal/provider/providers/aws/`)
- `aws-sdk-go-v2` (Secrets Manager), IRSA (sts.AssumeRoleProvider)
- Build tag: `go build -tags aws` — SDK only in cloud builds
- Test: LocalStack in docker-compose

**2b: GCP Provider** (`internal/provider/providers/gcp/`)
- GCP Secret Manager SDK, Workload Identity Federation
- Test: fake GCP server

**2c: Azure Provider** (`internal/provider/providers/azure/`)
- `azure-sdk-for-go` (Key Vault), Managed Identity
- Test: Azurite

### Phase 3: Optional Enhancements

Additional runtime improvements or observability only if demonstrated user
demand exists. No control-plane components unless they solve a real user
problem.

Examples that may be considered later:

- Sidecar hot-reload (inotify + polling).
- Prometheus metrics endpoint (`/metrics`).
- Advanced audit logging beyond structured JSON logs.

### Phase Architecture

```
Phase 1                Phase 2                Phase 3
┌──────────────┐      ┌──────────────┐       ┌──────────────┐
│  env         │      │  aws         │       │  optional    │
│  local       │      │  gcp         │       │  runtime     │
│  k8s         │ ───► │  azure       │ ───►  │  improvements│
│  webhook     │      │  vault       │       │  (only if    │
│  mutation    │      │  build tags  │       │  demand)     │
└──────────────┘      └──────────────┘       └──────────────┘

## Release Workflow

Before preparing a release:

- Update README if behavior changed.
- Update THREAT_MODEL.md if security assumptions changed.
- Update CHANGELOG.
- Run:
  - `make check`
  - `make vuln`
  - `make e2e`

Push a version tag (e.g. `v0.2.0`) to trigger the GitHub Actions release workflow.
```
