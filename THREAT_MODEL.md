# CHUR Threat Model

This document describes the security model of `chur` as of v0.2.

## Security Goals

CHUR aims to:

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
| Provider credentials | Provider-specific (env, file, cloud IAM) | CHUR does not store them. |

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
- **Mitigation:** CHUR writes secrets to an `emptyDir` volume with
  `medium: Memory`. The value never touches the container image, is not stored
  in etcd by CHUR, and is not written to a hostPath volume.
- **Note:** When using the `k8s` provider, the upstream Kubernetes Secret still
  exists in etcd. CHUR changes the delivery mechanism, not the storage backend.

### T2: Secret leak via environment variables

- **Scenario:** Secret is exposed to the application container through env vars.
- **Mitigation:** CHUR never injects the secret value into the application
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
- **Mitigation:** `chur-init` runs as non-root (`runAsUser: 1001`), with a
  read-only root filesystem, dropped capabilities, and a shared fsGroup. The
  secret file is written with mode `0640` so only the shared group can read it.

### T5: Secret exfiltration by a compromised app container

- **Scenario:** A compromised app container reads another workload's secret.
- **Mitigation:** The secret is scoped to the Pod's tmpfs. A container in a
  different Pod cannot access it. Within the same Pod, all containers share the
  tmpfs; this is by design for sidecar/helper patterns.

### T6: Oversized secret causing denial of service

- **Scenario:** A huge secret exhausts node memory or init container time.
- **Mitigation:** `CHUR_MAX_SECRET_SIZE` limits the size of a fetched secret.
  `CHUR_VOLUME_SIZE_LIMIT` bounds the tmpfs volume.

### T7: Denial of service against the webhook

- **Scenario:** A flood of admission reviews exhausts webhook memory or threads.
- **Mitigation:** The webhook limits concurrent requests
  (`CHUR_MAX_CONCURRENT`) and request headers (`MaxHeaderBytes`). Timeouts are
  configured on the HTTP server.

### T8: Information disclosure through logs

- **Scenario:** Secret values are logged.
- **Mitigation:** CHUR logs structured metadata only (provider, reference, path,
  bytes). Secret values are never logged.

## Non-Goals

The following are intentionally out of scope for v0.2:

- Secret rotation without Pod restart.
- Lease or ownership model for secrets.
- A custom secret storage backend.
- A custom authorization model or policy engine.
- Advanced audit capabilities beyond structured JSON logs.
- Control-plane components, CRDs, or controllers.

## Basic Audit

CHUR emits structured JSON logs from both `chur-webhook` and `chur-init`. These
logs are the basic audit trail: they record when a secret is injected, by which
provider, and for which reference. Advanced audit capabilities (for example,
streaming to a SIEM or Kubernetes Audit Events integration) are not implemented
in v0.2.
