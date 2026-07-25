package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
)

func TestLocalProvider_GetSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := &LocalProvider{basePath: dir, maxSize: 1 << 20}
	ctx := context.Background()

	secret, err := p.GetSecret(ctx, "secret.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(secret))
	}

	_, err = p.GetSecret(ctx, "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLocalProvider_GetSecret_PathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// File outside basePath should not be reachable.
	if err := os.WriteFile(filepath.Join(dir, "..", "outside.txt"), []byte("leak"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := &LocalProvider{basePath: dir, maxSize: 1 << 20}
	ctx := context.Background()

	_, err := p.GetSecret(ctx, "../outside.txt")
	if err == nil {
		t.Fatal("expected validation error for path traversal ref")
	}
}

func TestLocalProviderCancelContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := &LocalProvider{basePath: dir, maxSize: 1 << 20}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.GetSecret(ctx, "secret.txt")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestLocalProviderInvalidBasePath(t *testing.T) {
	t.Parallel()
	tests := []string{
		"/",
		"/..",
		"/../etc",
	}
	for _, basePath := range tests {
		t.Run(basePath, func(t *testing.T) {
			err := validate.ValidateLocalBasePath(basePath)
			if err == nil {
				t.Errorf("expected error for basePath %q", basePath)
			}
		})
	}
}

func TestLocalProviderInitRejectsRoot(t *testing.T) {
	t.Setenv("CHUR_LOCAL_BASE_PATH", "/")
	f, ok := provider.Get("local")
	if !ok {
		t.Fatal("local provider not registered")
	}
	_, err := f(context.Background())
	if err == nil {
		t.Fatal("expected error for CHUR_LOCAL_BASE_PATH=/")
	}
}
