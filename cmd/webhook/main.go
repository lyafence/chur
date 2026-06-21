package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lyafence/chur/internal/webhook"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv, err := webhook.NewServer()
	if err != nil {
		slog.Error("failed to create webhook server", "error", err)
		os.Exit(1)
	}

	listenAddr := os.Getenv("CHUR_LISTEN")
	if listenAddr == "" {
		listenAddr = ":8443"
	}

	tlsMode := webhook.TLSModeDev
	if os.Getenv("CHUR_TLS_MODE") == "prod" {
		tlsMode = webhook.TLSModeProd
	}

	httpSrv := &http.Server{
		Addr:      listenAddr,
		Handler:   srv,
		TLSConfig: webhook.TLSConfig(tlsMode),
	}

	go func() {
		slog.Info("starting chur-webhook",
			"version", version, "addr", httpSrv.Addr, "tls_mode", tlsMode)
		if err := httpSrv.ListenAndServeTLS("/etc/chur/tls/tls.crt", "/etc/chur/tls/tls.key"); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	if err := httpSrv.Shutdown(context.Background()); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}
