package main

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/lyafence/chur/internal/provider"
	_ "github.com/lyafence/chur/internal/providers/env"
	_ "github.com/lyafence/chur/internal/providers/k8s"
	_ "github.com/lyafence/chur/internal/providers/local"
	"github.com/lyafence/chur/internal/validate"
)

var version = "dev"

const maxRetries = 5

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("starting chur-init", "version", version)

	providerName := os.Getenv("CHUR_PROVIDER")
	if providerName == "" {
		providerName = "env"
	}
	secretRef := os.Getenv("CHUR_SECRET_REF")
	if secretRef == "" {
		slog.Error("CHUR_SECRET_REF is required")
		os.Exit(1)
	}
	if err := validate.ValidateSecretRef(secretRef); err != nil {
		slog.Error("invalid CHUR_SECRET_REF", "error", err)
		os.Exit(1)
	}
	mountPath := os.Getenv("CHUR_MOUNT_PATH")
	if mountPath == "" {
		mountPath = "/secrets"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	factory, ok := provider.Get(providerName)
	if !ok {
		slog.Error("unknown provider", "provider", providerName)
		os.Exit(1)
	}

	secret, err := backoffFetch(ctx, factory, secretRef)
	if err != nil {
		slog.Error("failed to get secret", "ref", secretRef, "error", err)
		os.Exit(1)
	}

	path := filepath.Join(mountPath, secretRef)
	if err := os.MkdirAll(mountPath, 0700); err != nil {
		slog.Error("failed to create mount dir", "path", mountPath, "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, secret, 0600); err != nil {
		slog.Error("failed to write secret", "path", path, "error", err)
		os.Exit(1)
	}

	slog.Info("secret injected", "provider", providerName, "ref", secretRef, "path", path, "bytes", len(secret))
}

// backoffFetch fetches a secret with exponential backoff retry.
// Network may not be ready immediately in init containers; retry with jitter.
func backoffFetch(ctx context.Context, factory provider.Factory, secretRef string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			slog.Info("retrying secret fetch", "attempt", attempt+1, "max", maxRetries, "delay", delay.String())
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		p, err := factory(ctx)
		if err != nil {
			lastErr = err
			continue
		}

		secret, err := p.GetSecret(ctx, secretRef)
		if err != nil {
			lastErr = err
			continue
		}

		return secret, nil
	}

	return nil, lastErr
}
