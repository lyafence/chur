# Contributing

Contributions are welcome. Please open an issue first to discuss changes.

## Development

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run `make check` (uses the required build tags; `go test ./...` directly will miss some tests)
5. Open a pull request

## Code Style

- Go: standard `gofmt` + `golangci-lint`
- Follow the existing patterns in the codebase
- All exported symbols must have doc comments

## End-to-End Tests

```bash
# Build images, spin up a Kind cluster, deploy via Helm, and verify injection
make e2e

# Keep the cluster for debugging after tests
E2E_SKIP_CLEANUP=true make e2e
# Cleanup: kind delete cluster --name chur-e2e
```
