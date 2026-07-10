package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetSecretViaExec(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "get-secret.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf "secret-for-$1"
`), 0755); err != nil {
		t.Fatal(err)
	}

	b := New(script, 0, 1<<20)
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

	b := New(script, 50*time.Millisecond, 1<<20)
	_, err := b.GetSecret(context.Background(), "test")
	if err == nil {
		t.Error("expected timeout error")
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

	b := New(script, 0, 3)
	_, err := b.GetSecret(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when stdout exceeds max")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected max size error, got %v", err)
	}
}
