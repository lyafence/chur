package main

import (
	"context"
	"log/slog"
	"net"
	"os"

	"golang.org/x/net/netutil"

	"github.com/lyafence/chur/internal/keeper"
	"github.com/lyafence/chur/internal/keeper/exec"
	"github.com/lyafence/chur/internal/keeper/filesystem"
)

var version = "dev"

func main() {
	ctx := context.Background()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := keeper.ConfigFromEnv()
	if err != nil {
		slog.ErrorContext(ctx, "invalid keeper config", "error", err)
		os.Exit(1)
	}

	switch cfg.BackendType {
	case "filesystem":
		root := os.Getenv("CHUR_KEEPER_BACKEND_FS_ROOT")
		if root == "" {
			root = "/var/lib/chur-keeper/secrets"
		}
		cfg.Backend = filesystem.NewWithMaxSize(root, cfg.MaxSecretSize)

	case "exec":
		cmd := cfg.ExecCommand
		if cmd == "" {
			slog.ErrorContext(ctx, "CHUR_KEEPER_EXEC_COMMAND is required for exec backend")
			os.Exit(1)
		}
		cfg.Backend = exec.New(cmd, cfg.ExecTimeout, cfg.ExecMaxStdout)

	default:
		slog.ErrorContext(ctx, "unknown backend type", "backend", cfg.BackendType)
		os.Exit(1)
	}

	tlsCfg, cleanup, err := keeper.ServerTLSConfig(ctx, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "keeper tls config failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := cleanup(); err != nil {
			slog.ErrorContext(ctx, "keeper: failed to clean up temp tls dir", "error", err)
		}
	}()

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		slog.ErrorContext(ctx, "keeper: listen failed", "addr", cfg.Listen, "error", err)
		os.Exit(1)
	}
	listener = netutil.LimitListener(listener, cfg.MaxConcurrent)
	defer listener.Close()

	slog.InfoContext(ctx, "keeper configuration loaded",
		"version", version,
		"listen", cfg.Listen,
		"health_listen", cfg.HealthListen,
		"tls_mode", cfg.TLSMode,
		"backend", cfg.Backend.Name(),
	)

	if err := keeper.Serve(ctx, cfg, tlsCfg, listener); err != nil {
		slog.ErrorContext(ctx, "keeper exited", "error", err)
		os.Exit(1)
	}
}
