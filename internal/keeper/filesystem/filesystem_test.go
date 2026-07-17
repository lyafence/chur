package filesystem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustCreate(t *testing.T, root string, maxSize int64) *FSBackend {
	t.Helper()
	b, err := NewWithMaxSize(root, maxSize)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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

	b := mustCreate(t, dir, 1<<20)
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
	b := mustCreate(t, t.TempDir(), 1<<20)
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
	b := mustCreate(t, dir, 1<<20)
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
	b := mustCreate(t, root, 1<<20)
	_, err := b.GetSecret(context.Background(), "link")
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("expected path-escapes error, got %v", err)
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
	b := mustCreate(t, dir, 1<<20)
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
	b := mustCreate(t, dir, 5)
	_, err := b.GetSecret(context.Background(), "big")
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatal("expected max size error")
	}
}

func TestGetSecretInvalidRef(t *testing.T) {
	t.Parallel()
	b := mustCreate(t, t.TempDir(), 1<<20)
	for _, ref := range []string{"", "a\\b", "/abs", "../x"} {
		_, err := b.GetSecret(context.Background(), ref)
		if err == nil {
			t.Errorf("expected error for ref %q", ref)
		}
	}
}

func TestGetSecretPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	fullPath := filepath.Join(dir, "noread")
	if err := os.WriteFile(fullPath, []byte("secret"), 0000); err != nil {
		t.Fatal(err)
	}
	b := mustCreate(t, dir, 1<<20)
	_, err := b.GetSecret(context.Background(), "noread")
	if err == nil {
		t.Error("expected error for permission denied file")
	}
}

func TestGetSecretDirRef(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "mydir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	b := mustCreate(t, dir, 1<<20)
	_, err := b.GetSecret(context.Background(), "mydir")
	if err == nil {
		t.Error("expected error when ref points to directory")
	}
}
