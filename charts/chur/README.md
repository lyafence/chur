# chur Helm Chart

Universal Multi-Cloud & Bare-Metal Zero-Trust Secret Injector for Kubernetes.

## Requirements

- Kubernetes >= 1.28
- Helm 3

For the default TLS provider (`certManager`):
- [cert-manager](https://cert-manager.io) must be installed in the cluster.

## Installation

### Production (cert-manager)

```bash
helm install chur . --wait
```

Configure via `--set` or a custom values file.

### Development / CI (no cert-manager)

```bash
helm install chur . -f values-dev.yaml --wait
```

This uses Helm's built-in self-signed certificate generation. The private key
is stored in the Helm release Secret and a new certificate is generated on
every upgrade. **Not for production.**

### User-provided TLS certificate

```bash
helm install chur . \
  --set tls.provider=userSecret \
  --set tls.userSecret.name=my-tls-secret \
  --set tls.userSecret.caBundle="$(base64 -w0 < ca.crt)"
```

## TLS Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `webhook.tlsMode` | `server` | `server` (no client cert) or `mtls` (require API server client cert) |
| `tls.provider` | `certManager` | `certManager`, `helmGenerated`, or `userSecret` |
| `mtls.caBundle` | `""` | PEM-encoded CA cert for verifying the API server's client cert (mtls only) |

## RBAC

When using the `k8s` provider, the `chur-init` init container needs permission
to read Secrets. The chart can create the necessary RBAC resources:

- `ServiceAccount` `chur-init`
- `Role` `chur-secret-reader` (get on secrets)
- `RoleBinding`

Configure target namespaces via `rbac.namespaces`. If empty, RBAC is created
only in the release namespace. Your Pods must use the `chur-init` ServiceAccount:

```yaml
spec:
  serviceAccountName: chur-init
```

## Values

See `values.yaml` for the full list of configurable parameters.
