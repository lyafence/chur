package filesystem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

	b := &FSBackend{Root: dir, MaxSize: 1 << 20}
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

func TestFSBackend_RejectsSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "..", "secret")
	if err := os.WriteFile(target, []byte("leak"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink("../secret", link); err != nil {
		t.Fatal(err)
	}
	b := &FSBackend{Root: root}
	_, err := b.GetSecret(context.Background(), "link")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("expected symlink rejection")
	}
}

func TestFSBackend_NoFalsePositiveOnRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ref := "secret.txt"
	content := []byte("real-secret")
	if err := os.WriteFile(filepath.Join(dir, ref), content, 0644); err != nil {
		t.Fatal(err)
	}
	b := &FSBackend{Root: dir, MaxSize: 1 << 20}
	data, err := b.GetSecret(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("got %q, want %q", data, content)
	}
}

func TestGetSecretExceedsMaxSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big"), []byte("too-large-content"), 0644); err != nil {
		t.Fatal(err)
	}
	b := &FSBackend{Root: dir, MaxSize: 5}
	_, err := b.GetSecret(context.Background(), "big")
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatal("expected max size error")
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
