package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyafence/chur/internal/keeper"
	"github.com/lyafence/chur/internal/keeper/filesystem"
	keeperprovider "github.com/lyafence/chur/internal/providers/keeper"
)

func TestKeeperIntegration(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "test/db/password")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("integration-secret"), 0644); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()

	cfg := keeper.DefaultConfig()
	cfg.Listen = addr
	cfg.HealthListen = ""
	cfg.TLSMode = keeper.TLSModeSelfSigned
	cfg.BackendType = "filesystem"
	fsBackend, err := filesystem.NewWithMaxSize(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Backend = fsBackend

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tlsCfg, cleanup, err := keeper.ServerTLSConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cleanup()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- keeper.Serve(ctx, cfg, tlsCfg, listener)
	}()

	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if i == 9 {
			t.Fatalf("server did not start on %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	p, err := keeperprovider.NewProvider("https://"+addr, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reqCancel()

	data, err := p.GetSecret(reqCtx, "test/db/password")
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(data) != "integration-secret" {
		t.Errorf("got %q, want %q", string(data), "integration-secret")
	}

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("serve error: %v", err)
	}
}
