package main

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/lyafence/chur/internal/bytesize"
	"github.com/lyafence/chur/internal/provider"
	_ "github.com/lyafence/chur/internal/providers/env"
	_ "github.com/lyafence/chur/internal/providers/keeper"
	_ "github.com/lyafence/chur/internal/providers/local"
	"github.com/lyafence/chur/internal/validate"
)

var version = "dev"

const maxRetries = 5

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()

	slog.InfoContext(ctx, "starting chur-init", "version", version)

	providerName := os.Getenv("CHUR_PROVIDER")
	if providerName == "" {
		providerName = "env"
	}
	secretRef := os.Getenv("CHUR_SECRET_REF")
	mountPath := os.Getenv("CHUR_MOUNT_PATH")
	if mountPath == "" {
		mountPath = "/secrets"
	}

	if secretRef == "" {
		slog.ErrorContext(ctx, "CHUR_SECRET_REF is required")
		os.Exit(1)
	}
	validator := validate.ValidateSecretRef
	if providerName == "keeper" {
		validator = validate.ValidateKeeperRef
	}
	if err := validator(secretRef); err != nil {
		slog.ErrorContext(ctx, "invalid CHUR_SECRET_REF", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	factory, ok := provider.Get(providerName)
	if !ok {
		slog.ErrorContext(ctx, "unknown provider", "provider", providerName)
		os.Exit(1)
	}

	secret, err := backoffFetch(ctx, factory, secretRef)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get secret", "ref", secretRef, "error", err)
		os.Exit(1)
	}

	maxSizeStr := os.Getenv("CHUR_MAX_SECRET_SIZE")
	if maxSizeStr == "" {
		maxSizeStr = "1Mi"
	}
	maxBytes, err := bytesize.Parse(maxSizeStr)
	if err != nil {
		slog.ErrorContext(ctx, "invalid CHUR_MAX_SECRET_SIZE", "value", maxSizeStr, "error", err)
		os.Exit(1)
	}
	if int64(len(secret)) > maxBytes {
		slog.ErrorContext(ctx, "secret exceeds max size",
			"ref", secretRef, "size", len(secret), "max", maxSizeStr)
		os.Exit(1)
	}

	if err := validate.ValidateMountPath(mountPath); err != nil {
		slog.ErrorContext(ctx, "invalid CHUR_MOUNT_PATH", "path", mountPath, "error", err)
		os.Exit(1)
	}

	path := filepath.Join(mountPath, secretRef)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		slog.ErrorContext(ctx, "failed to create secret directory", "path", filepath.Dir(path), "error", err)
		os.Exit(1)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, secret, 0640); err != nil {
		os.Remove(tmpPath)
		slog.ErrorContext(ctx, "failed to write secret", "path", path, "error", err)
		os.Exit(1)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		slog.ErrorContext(ctx, "failed to rename secret", "path", path, "error", err)
		os.Exit(1)
	}
	if err := os.Chmod(path, 0640); err != nil {
		slog.ErrorContext(ctx, "failed to chmod secret", "path", path, "error", err)
		os.Exit(1)
	}

	slog.InfoContext(ctx, "secret injected", "provider", providerName, "ref", secretRef, "path", path, "bytes", len(secret))
}

// backoffFetch fetches a secret with exponential backoff retry.
// Network may not be ready immediately in init containers; retry with jitter.
func backoffFetch(ctx context.Context, factory provider.Factory, secretRef string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			slog.WarnContext(ctx, "retrying secret fetch", "attempt", attempt+1, "max", maxRetries, "delay", delay.String(), "error", lastErr)
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
