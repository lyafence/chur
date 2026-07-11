# chur Threat Model

This document describes the security model of `chur` (includes optional chur-keeper).

## Security Goals

chur aims to:

- Deliver secrets directly into container memory (tmpfs) without exposing them
  to the application environment variables.
- Avoid Kubernetes Secret volumes and hostPath-backed secret mounts in the
  application container.
- Keep the lifetime of secret material as short as possible: fetch on Pod start,
  write to tmpfs, then let the init container exit.
- Rely on standard Kubernetes primitives (MutatingWebhookConfiguration, init
  containers, emptyDir volumes) instead of introducing a custom control plane.
- Minimize the attack surface of both the webhook and the init container.

## Assets

| Asset | Location | Notes |
|-------|----------|-------|
| Secret material | tmpfs volume inside the Pod | Short-lived; destroyed with the Pod. |
| TLS credentials | Kubernetes Secret mounted to `chur-webhook` | Used for HTTPS admission endpoint. |
| Service account tokens | Projected into `chur-init` | Used by the `k8s` provider only. |
| Provider credentials | Provider-specific (env, file, cloud IAM) | chur does not store them. |

## Trust Boundaries

```
Kubernetes API Server
        │
        │ TLS admission review
        ▼
chur-webhook (Deployment)
        │
        │ JSON Patch: tmpfs volume + init container
        ▼
        Pod
        ├── chur-init (reads provider, writes secret to tmpfs)
        │       │
        │       └──(keeper)──► chur-keeper (optional, HTTPS)
        │                          │
        │                          ├── filesystem backend
        │                          └── exec backend
        └── app container (reads secret from tmpfs)
```

Trust assumptions:

- The Kubernetes control plane and node kernel are trusted.
- The cluster administrator trusts the `chur-webhook` image and TLS config.
- The application owner trusts the provider backend that stores the raw secret.

## Threats and Mitigations

### T1: Secret leak via etcd or disk

- **Scenario:** Secret value ends up in Kubernetes etcd, node disk, or container
  image layers.
- **Mitigation:** chur writes secrets to an `emptyDir` volume with
  `medium: Memory`. The value never touches the container image, is not stored
  in etcd by chur, and is not written to a hostPath volume.
- **Note:** When using the `k8s` provider, the upstream Kubernetes Secret still
  exists in etcd. chur changes the delivery mechanism, not the storage backend.

### T2: Secret leak via environment variables

- **Scenario:** Secret is exposed to the application container through env vars.
- **Mitigation:** chur never injects the secret value into the application
  container environment. The secret only exists as a file in tmpfs.

### T3: Unauthorized mutation of Pods

- **Scenario:** An attacker sends crafted admission reviews or bypasses the
  webhook.
- **Mitigation:** The webhook uses TLS. In production it should be deployed with
  `failurePolicy: Fail` so the API server rejects Pod creation if the webhook is
  unreachable. The webhook validates all annotations and rejects unknown
  providers.

### T4: Privilege escalation inside the Pod

- **Scenario:** The init container or app container runs as root and can read
  secrets belonging to other processes.
- **Mitigation:** `chur-init` runs as non-root (configurable via
  `CHUR_RUN_AS_USER`, `CHUR_RUN_AS_GROUP`, `CHUR_FS_GROUP`), with a read-only
  root filesystem, dropped capabilities, and a shared fsGroup. The secret file
  is written with mode `0640` so only the shared group can read it.

### T5: Secret exfiltration by a compromised app container

- **Scenario:** A compromised app container reads another workload's secret.
- **Mitigation:** The secret is scoped to the Pod's tmpfs. A container in a
  different Pod cannot access it. Within the same Pod, all containers share the
  tmpfs; this is by design for sidecar/helper patterns.

### T6: Oversized secret causing denial of service

- **Scenario:** A huge secret exhausts node memory or init container time.
- **Mitigation:** `CHUR_MAX_SECRET_SIZE` limits the size of a fetched secret.
  `CHUR_VOLUME_SIZE_LIMIT` bounds the tmpfs volume. The optional `chur-keeper`
  also enforces its own limit via `CHUR_KEEPER_MAX_SECRET_SIZE` (server-side).

### T7: Denial of service against the webhook

- **Scenario:** A flood of admission reviews exhausts webhook memory or threads.
- **Mitigation:** The webhook limits concurrent requests
  (`CHUR_MAX_CONCURRENT`) and request headers (`MaxHeaderBytes`). Timeouts are
  configured on the HTTP server.

### T8: Information disclosure through logs

- **Scenario:** Secret values are logged.
- **Mitigation:** chur logs structured metadata only (provider, reference, path,
  bytes). Secret values are never logged.

### T9: Secret leak or abuse via keeper backend

- **Scenario:** `chur-keeper` reads secrets from arbitrary files or executes
  arbitrary commands due to a malicious `ref`.
- **Mitigation:** Keeper refs are validated to disallow traversal (`..`),
  absolute paths, and control characters. The filesystem backend resolves the
  path and verifies it remains under `CHUR_KEEPER_BACKEND_FS_ROOT`.
- **Note:** Symlinks inside `CHUR_KEEPER_BACKEND_FS_ROOT` are rejected at the
  filesystem level — both at open time (`os.Lstat`) and after open (`f.Stat` +
  `os.SameFile`), closing the TOCTOU race window between path validation and
  file read. Operators should still ensure the root directory is writable only
  by trusted principals.
- **Note:** For the `exec` backend, `chur-keeper` passes `ref` as a single
  isolated argument, which prevents shell injection. However, the target
  executable or script is responsible for validating and sanitizing the
  dynamic `ref` parameter to avoid downstream directory traversal,
  command injection, or application-level exploits.

## Non-Goals

The following are intentionally out of scope:

- Secret rotation without Pod restart.
- Lease or ownership model for secrets.
- A custom secret storage backend.
- A custom authorization model or policy engine.
- Advanced audit capabilities beyond structured JSON logs.
- Control-plane components, CRDs, or controllers.
- Automated mTLS certificate rotation for `chur-keeper`.

## Basic Audit

chur emits structured JSON logs from both `chur-webhook` and `chur-init`. These
logs are the basic audit trail: they record when a secret is injected, by which
provider, and for which reference. Advanced audit capabilities (for example,
streaming to a SIEM or Kubernetes Audit Events integration) are not implemented
in v0.2.
