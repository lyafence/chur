# Operations Guide

## Architecture Overview

See [README.md](README.md) for the architecture diagram and component descriptions.

## Health Endpoints

| Component | Endpoint | Port | Purpose | Verified |
|-----------|----------|------|---------|----------|
| chur-webhook | `/healthz`, `/readyz` | 8080 | Liveness and readiness probes | `cmd/webhook/main.go` |
| chur-webhook | `/metrics` | 8080 | Prometheus metrics | `internal/metrics/handler.go` |
| chur-keeper | `/healthz` | 9444 | Liveness probe | `internal/keeper/config.go:38` |
| chur-keeper | `/metrics` | 9444 | Prometheus metrics | `internal/metrics/handler.go` |

Both endpoints return HTTP 200 with `{"status":"ok"}` when healthy.

## Debugging

### Pod stuck in Init

```
1. Verify mutation was applied:

   kubectl get pod <pod-name> -o yaml | grep -A5 initContainers

   Expected: a `chur-init` init container with a tmpfs volume mount.

2. Check init container logs:

   kubectl logs <pod-name> -c chur-init

3. If the provider is `keeper`, check keeper logs:

   kubectl logs -l app.kubernetes.io/component=keeper

4. If the webhook did not mutate the pod, check webhook logs:

   kubectl logs -l app.kubernetes.io/component=webhook
```

### Pod created without secret

```
1. Verify pod has chur annotations:

   kubectl get pod <pod-name> -o yaml | grep 'chur.io/'

2. If annotations are missing, check the pod spec and add them.

3. If annotations are present but no mutation occurred:

   kubectl logs -l app.kubernetes.io/component=webhook --tail=50
```

### Keeper returns errors

```
1. Check keeper logs:

   kubectl logs -l app.kubernetes.io/component=keeper

2. Verify mTLS configuration if enabled.

3. Verify the exec command or filesystem path is correct.
```

## Troubleshooting Matrix

| Symptom | Likely Cause | Check |
|---------|-------------|-------|
| Init container crash loop | Provider error / secret not found | Init container logs, provider configuration |
| Pod not mutated | Webhook unreachable, failurePolicy=Ignore, or annotation missing | Webhook logs, TLS cert, pod annotations |
| 403 from keeper | mTLS client certificate mismatch | Keeper logs, client cert/key paths |
| Init timeout (>=30s) | Keeper unavailable, exec backend slow, network issue | Keeper logs, network connectivity, exec script |
| Admission error: "unknown provider" | Typo in `chur.io/provider` annotation | Annotation value, valid providers list |
| Secret file empty | Wrong `chur.io/secret-key` for k8s provider | K8s Secret data keys, annotation value |
