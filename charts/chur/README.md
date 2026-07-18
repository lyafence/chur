# chur Helm Chart

Lightweight in-memory secret injection for Kubernetes.

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
helm install chur . --set tls.provider=helmGenerated --wait
```

This uses Helm's built-in self-signed certificate generation. A new certificate
is generated on every upgrade. **Not for production.**

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

## Keeper (optional)

The `keeper` section controls the optional `chur-keeper` deployment:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `keeper.enabled` | `false` | Enable the chur-keeper deployment and service |
| `keeper.replicaCount` | `1` | Number of keeper pod replicas |
| `keeper.image.repository` | `ghcr.io/lyafence/chur-keeper` | Keeper image repository |
| `keeper.image.tag` | Chart `appVersion` | Keeper image tag |
| `keeper.image.pullPolicy` | `IfNotPresent` | Keeper image pull policy |
| `keeper.listen` | `:9443` | Keeper HTTPS listen address |
| `keeper.healthListen` | `:9444` | Keeper health probe listen address |
| `keeper.tlsMode` | `self-signed` | TLS mode: `self-signed` or `mtls` |
| `keeper.backend` | `filesystem` | Secret storage backend: `filesystem` or `exec` |
| `keeper.maxSecretSize` | `"1Mi"` | Maximum secret response size |
| `keeper.maxConcurrent` | `100` | Maximum concurrent HTTP requests |
| `keeper.fsRoot` | `/var/lib/chur-keeper/secrets` | Root directory (filesystem backend) |
| `keeper.execCommand` | `""` | Exec command (required when backend=exec) |
| `keeper.execTimeout` | `10` | Exec command timeout in seconds |
| `keeper.execMaxStdout` | `1048576` | Max stdout bytes for exec backend |
| `keeper.volume.hostPath.path` | `/var/lib/chur-keeper/secrets` | Host path for filesystem backend |
| `keeper.volume.hostPath.type` | `DirectoryOrCreate` | Host path type |
| `keeper.tls.existingSecret` | `""` | Existing TLS Secret name (required for `mtls`) |
| `keeper.tls.certPath` | `/etc/chur-keeper/tls/tls.crt` | TLS cert path inside keeper container |
| `keeper.tls.keyPath` | `/etc/chur-keeper/tls/tls.key` | TLS key path inside keeper container |
| `keeper.mtls.enabled` | `false` | Enable mTLS client cert injection for chur-init |
| `keeper.mtls.clientCA.existingSecret` | `""` | Existing Secret with CA bundle for verifying mTLS client certs |
| `keeper.mtls.clientCA.existingConfigMap` | `""` | Alternative to existingSecret — ConfigMap with CA bundle |
| `keeper.mtls.clientCA.path` | `/etc/chur-keeper/client-ca/ca.crt` | Client CA mount path inside keeper container |
| `keeper.mtls.clientCert.secretName` | `""` | Kubernetes Secret name with client cert for chur-init |
| `keeper.mtls.clientCert.mountPath` | `/etc/chur-keeper/client-tls` | Mount path for client cert |
| `keeper.mtls.clientCert.certKey` | `tls.crt` | Key name in Secret for the TLS cert |
| `keeper.mtls.clientCert.keyKey` | `tls.key` | Key name in Secret for the TLS key |
| `keeper.mtls.clientCAPath` | `/etc/chur-keeper/server-ca/ca.crt` | Path to server CA for chur-init |
| `keeper.clientTLS.existingSecret` | `""` | Existing Secret (tls.crt + tls.key) for keeper's server TLS identity |
| `keeper.service.type` | `ClusterIP` | Keeper service type |
| `keeper.service.port` | `9443` | Keeper service HTTPS port |
| `keeper.extraVolumes` | `[]` | Extra volumes for the keeper pod |
| `keeper.extraVolumeMounts` | `[]` | Extra volume mounts for the keeper container |
| `keeper.extraEnv` | `[]` | Extra env vars for the keeper container |
| `keeper.extraInitContainers` | `[]` | Extra init containers for the keeper pod. Useful for installing CLI tools (`vault`, `aws`, `gcloud`) in a shared emptyDir before keeper starts |
| `keeper.resources` | `{}` | Keeper container resource limits |

### extraInitContainers

When the exec backend needs a tool not present in the distroless keeper image,
use `extraInitContainers` to provide it via a shared emptyDir before keeper
starts:

```yaml
keeper:
  enabled: true
  backend: exec
  execCommand: /usr/local/bin/helper
  extraInitContainers:
    - name: prepare-helper
      image: your-helper-image
      command:
        - sh
        - -c
        - |
          # Prepare /shared/helper here (download, copy, or generate)
      volumeMounts:
        - name: shared-bin
          mountPath: /shared
  extraVolumes:
    - name: shared-bin
      emptyDir: {}
  extraVolumeMounts:
    - name: shared-bin
      mountPath: /usr/local/bin
```

When `keeper.enabled=true`, the webhook automatically injects `CHUR_KEEPER_URL`
into chur-init containers of pods using the keeper provider
(`chur.io/provider: keeper`). Use `chur.io/provider: keeper` in your Pod
annotations to route secret fetching through the keeper.

Additional keeper annotations:

| Annotation | Effect |
|---|---|
| `chur.io/keeper-skip-verify: "1"` or `"true"` | Injects `CHUR_KEEPER_SKIP_VERIFY=1` (dev, skips TLS verification) |

When `keeper.mtls.enabled=true`, the webhook automatically injects
`CHUR_KEEPER_TLS_CERT_PATH`, `CHUR_KEEPER_TLS_KEY_PATH`, and
`CHUR_KEEPER_SERVER_CA` — no `chur.io/provider-env` annotation needed
for basic mTLS. Set `keeper.mtls.clientCert.secretName` to have the
webhook auto-mount the client certificate secret into the init container.

## Values

See `values.yaml` for the full list of configurable parameters.
