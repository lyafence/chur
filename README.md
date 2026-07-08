# Chur Helm Charts

_Lightweight in-memory secret injection for Kubernetes_

[![GitHub release](https://img.shields.io/github/v/release/lyafence/chur?style=flat-square)](https://github.com/lyafence/chur/releases)
[![License](https://img.shields.io/github/license/lyafence/chur?style=flat-square)](https://github.com/lyafence/chur/blob/main/LICENSE)

This is the Helm chart repository for [Chur](https://github.com/lyafence/chur).

## Quick Start

Add the repository and install the chart:

```bash
helm repo add chur https://lyafence.github.io/chur
helm repo update
helm install chur chur/chur --namespace chur-system --create-namespace
```

## Docker Images

Multi-platform images are published to GitHub Container Registry:

| Image | Purpose |
|-------|---------|
| `ghcr.io/lyafence/chur-webhook` | Admission webhook server |
| `ghcr.io/lyafence/chur-init` | Init container for secret injection |

Supported platforms: `linux/amd64`, `linux/arm64`

## Documentation

For architecture details, provider configuration, mTLS, RBAC, and advanced examples, see the [main repository](https://github.com/lyafence/chur).

## License

[MIT License](https://github.com/lyafence/chur/blob/main/LICENSE)
