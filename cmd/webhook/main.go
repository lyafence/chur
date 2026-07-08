package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
	if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
		cfg.MaxSecretSize = v
	}
	if v := os.Getenv("CHUR_LOCAL_BASE_PATH"); v != "" {
		cfg.LocalBasePath = v
	}
	if v := os.Getenv("CHUR_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			slog.Error("invalid CHUR_MAX_CONCURRENT", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.MaxConcurrent = n
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

	tlsMode := webhook.TLSModeServer
	switch v := os.Getenv("CHUR_TLS_MODE"); v {
	case "mtls":
		tlsMode = webhook.TLSModeMTLS
	case "server", "":
		tlsMode = webhook.TLSModeServer
	default:
		slog.Error("invalid CHUR_TLS_MODE: must be 'server' or 'mtls'", "value", v)
		os.Exit(1)
	}

	var clientCAPEM []byte
	if tlsMode == webhook.TLSModeMTLS {
		caPath := os.Getenv("CHUR_CLIENT_CA_PATH")
		if caPath == "" {
			caPath = "/etc/chur/ca.crt"
		}
		var err error
		clientCAPEM, err = os.ReadFile(caPath)
		if err != nil {
			slog.Error("failed to read client CA", "path", caPath, "error", err)
			os.Exit(1)
		}
	}

	tlsCfg, err := webhook.TLSConfig(tlsMode, clientCAPEM)
	if err != nil {
		slog.Error("failed to build TLS config", "error", err)
		os.Exit(1)
	}

	certFile := "/etc/chur/tls/tls.crt"
	keyFile := "/etc/chur/tls/tls.key"

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		if os.Getenv("CHUR_TLS_AUTO_GENERATE") == "1" {
			dnsName := os.Getenv("CHUR_TLS_CERT_DNS_NAME")
			if dnsName == "" {
				dnsName = "localhost"
			}
			slog.Warn("TLS cert not found, generating self-signed certificate",
				"dns_name", dnsName, "path", certFile)
			tmpDir, err := os.MkdirTemp("", "chur-tls-*")
			if err != nil {
				slog.Error("failed to create temp dir for dev cert", "error", err)
				os.Exit(1)
			}
			defer func() {
				if err := os.RemoveAll(tmpDir); err != nil {
					slog.Error("failed to cleanup temp dir", "path", tmpDir, "error", err)
				}
			}()
			certFile = tmpDir + "/tls.crt"
			keyFile = tmpDir + "/tls.key"
			if err := webhook.GenerateTLSCert(dnsName, certFile, keyFile); err != nil {
				slog.Error("failed to generate dev TLS cert", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Error("TLS cert not found and CHUR_TLS_AUTO_GENERATE is not set",
				"path", certFile, "hint", "mount certs to /etc/chur/tls or set CHUR_TLS_AUTO_GENERATE=1")
			os.Exit(1)
		}
	}

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           srv,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
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
		if err := httpSrv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("health server shutdown error", "error", err)
	}
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("admission server shutdown error", "error", err)
	}
}
