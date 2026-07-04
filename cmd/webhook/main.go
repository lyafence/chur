package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lyafence/chur/internal/webhook"
	"k8s.io/apimachinery/pkg/api/resource"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := webhook.DefaultConfig()

	if v := os.Getenv("CHUR_VOLUME_SIZE_LIMIT"); v != "" {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			slog.Error("invalid CHUR_VOLUME_SIZE_LIMIT", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.VolumeSizeLimit = q
	}

	if v := os.Getenv("CHUR_ALLOWED_NAMESPACES"); v != "" {
		for _, ns := range strings.Split(v, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				cfg.AllowedNamespaces = append(cfg.AllowedNamespaces, ns)
			}
		}
	}

	if v := os.Getenv("CHUR_INIT_IMAGE"); v != "" {
		cfg.InitImage = v
	}

	srv, err := webhook.NewServer(cfg)
	if err != nil {
		slog.Error("failed to create webhook server", "error", err)
		os.Exit(1)
	}

	listenAddr := os.Getenv("CHUR_LISTEN")
	if listenAddr == "" {
		listenAddr = ":8443"
	}
	healthAddr := os.Getenv("CHUR_HEALTH_LISTEN")
	if healthAddr == "" {
		healthAddr = ":8080"
	}

	tlsMode := webhook.TLSModeDev
	if os.Getenv("CHUR_TLS_MODE") == "prod" {
		tlsMode = webhook.TLSModeProd
	}

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           srv,
		TLSConfig:         webhook.TLSConfig(tlsMode),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	healthSrv := &http.Server{
		Addr:              healthAddr,
		Handler:           webhook.HealthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		slog.Info("starting chur-webhook admission",
			"version", version, "addr", httpSrv.Addr, "tls_mode", tlsMode)
		if err := httpSrv.ListenAndServeTLS("/etc/chur/tls/tls.crt", "/etc/chur/tls/tls.key"); err != nil && err != http.ErrServerClosed {
			slog.Error("admission server error", "error", err)
			cancel()
		}
	}()

	go func() {
		slog.Info("starting chur-webhook health", "addr", healthSrv.Addr)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	if err := healthSrv.Shutdown(context.Background()); err != nil {
		slog.Error("health server shutdown error", "error", err)
	}
	if err := httpSrv.Shutdown(context.Background()); err != nil {
		slog.Error("admission server shutdown error", "error", err)
	}
}
