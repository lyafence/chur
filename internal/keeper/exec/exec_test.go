package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustCreate(t *testing.T, cmd string, timeout time.Duration, maxStdout int64) *ExecBackend {
	t.Helper()
	b, err := New(cmd, timeout, maxStdout)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGetSecretViaExec(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "get-secret.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf "secret-for-$1"
`), 0755); err != nil {
		t.Fatal(err)
	}

	b := mustCreate(t, script, 0, 1<<20)
	data, err := b.GetSecret(context.Background(), "db/password")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-for-db/password" {
		t.Errorf("got %q, want %q", string(data), "secret-for-db/password")
	}
}

func TestExecTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
sleep 10
printf done
`), 0755); err != nil {
		t.Fatal(err)
	}

	b := mustCreate(t, script, 50*time.Millisecond, 1<<20)
	_, err := b.GetSecret(context.Background(), "test")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestNewRejectsZeroMaxStdout(t *testing.T) {
	t.Parallel()
	_, err := New("echo", 0, 0)
	if err == nil {
		t.Fatal("expected error for maxStdout=0")
	}
	if !strings.Contains(err.Error(), "maxStdout") {
		t.Errorf("expected maxStdout error, got %v", err)
	}
}

func TestExecMaxStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "long.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf "abcdef"
`), 0755); err != nil {
		t.Fatal(err)
	}

	b := mustCreate(t, script, 0, 3)
	_, err := b.GetSecret(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when stdout exceeds max")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected max size error, got %v", err)
	}
}

func TestExecCommandNotFound(t *testing.T) {
	t.Parallel()
	b := mustCreate(t, "/nonexistent/binary", 0, 1<<20)
	_, err := b.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Error("expected error for non-existent command")
	}
}

func TestExecNonZeroExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
echo "something broke" >&2
exit 42
`), 0755); err != nil {
		t.Fatal(err)
	}

	b := mustCreate(t, script, 0, 1<<20)
	_, err := b.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Error("expected error for non-zero exit")
	}
}
