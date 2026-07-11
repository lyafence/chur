# chur Helm Charts

_Lightweight in-memory secret injection for Kubernetes_

[![GitHub release](https://img.shields.io/github/v/release/lyafence/chur?style=flat-square)](https://github.com/lyafence/chur/releases)
[![License](https://img.shields.io/github/license/lyafence/chur?style=flat-square)](https://github.com/lyafence/chur/blob/main/LICENSE)

This is the Helm chart repository for [chur](https://github.com/lyafence/chur).

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
| `ghcr.io/lyafence/chur-keeper` | Optional HTTPS secret gateway |

Supported platforms: `linux/amd64`, `linux/arm64`

## Documentation

- [Project README](https://github.com/lyafence/chur#readme) — architecture, providers, security model
- [Chart README](https://github.com/lyafence/chur/blob/main/charts/chur/README.md) — Helm installation, TLS modes, configuration
- [Configuration reference](https://github.com/lyafence/chur/blob/main/.env.example) — all environment variables
- [Helm values](https://github.com/lyafence/chur/blob/main/charts/chur/values.yaml) — chart values with defaults and descriptions

## License

[MIT License](https://github.com/lyafence/chur/blob/main/LICENSE)
