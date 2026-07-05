package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalProvider_GetSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p := &LocalProvider{basePath: dir}
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

	p := &LocalProvider{basePath: dir}
	ctx := context.Background()

	_, err := p.GetSecret(ctx, "../outside.txt")
	if err == nil {
		t.Fatal("expected validation error for path traversal ref")
	}
}
