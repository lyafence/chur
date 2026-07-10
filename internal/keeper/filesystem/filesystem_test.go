package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSecretFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ref := "prod/db/password"
	fullPath := filepath.Join(dir, ref)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte("supersecret"), 0644); err != nil {
		t.Fatal(err)
	}

	b := &FSBackend{Root: dir}
	data, err := b.GetSecret(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "supersecret" {
		t.Errorf("got %q, want %q", string(data), "supersecret")
	}
}

func TestGetSecretNotFound(t *testing.T) {
	t.Parallel()
	b := &FSBackend{Root: t.TempDir()}
	_, err := b.GetSecret(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent ref")
	}
}

func TestGetSecretPathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	b := &FSBackend{Root: dir}
	_, err := b.GetSecret(context.Background(), "../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestGetSecretInvalidRef(t *testing.T) {
	t.Parallel()
	b := &FSBackend{Root: t.TempDir()}
	for _, ref := range []string{"", "a\\b", "/abs", "../x"} {
		_, err := b.GetSecret(context.Background(), ref)
		if err == nil {
			t.Errorf("expected error for ref %q", ref)
		}
	}
}
