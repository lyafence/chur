package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/netutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/lyafence/chur/internal/tls"
	"github.com/lyafence/chur/internal/webhook"
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
			slog.ErrorContext(ctx, "invalid CHUR_VOLUME_SIZE_LIMIT", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.VolumeSizeLimit = q
	}

	if v := os.Getenv("CHUR_ALLOWED_NAMESPACES"); v != "" {
		for _, ns := range strings.Split(v, ",") {
			ns = strings.TrimSpace(ns)
			if ns == "" {
				continue
			}
			if err := validateDNS1123Label(ns); err != nil {
				slog.ErrorContext(ctx, "invalid namespace in CHUR_ALLOWED_NAMESPACES",
					"namespace", ns, "error", err)
				os.Exit(1)
			}
			cfg.AllowedNamespaces = append(cfg.AllowedNamespaces, ns)
		}
	}

	if v := os.Getenv("CHUR_INIT_IMAGE"); v != "" {
		cfg.InitImage = v
	}
	if v := os.Getenv("CHUR_INIT_IMAGE_PULL_POLICY"); v != "" {
		cfg.InitImagePullPolicy = corev1.PullPolicy(v)
	}
	if v := os.Getenv("CHUR_RUN_AS_USER"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_RUN_AS_USER", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.RunAsUser = n
	}
	if v := os.Getenv("CHUR_RUN_AS_GROUP"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_RUN_AS_GROUP", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.RunAsGroup = ptr.To(n)
	}
	if v := os.Getenv("CHUR_FS_GROUP"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_FS_GROUP", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.FSGroup = n
	}
	if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
		if _, err := resource.ParseQuantity(v); err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_MAX_SECRET_SIZE", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.MaxSecretSize = v
	}
	if v := os.Getenv("CHUR_LOCAL_BASE_PATH"); v != "" {
		cfg.LocalBasePath = v
	}
	if v := os.Getenv("CHUR_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			slog.ErrorContext(ctx, "invalid CHUR_MAX_CONCURRENT", "value", v, "error", err)
			os.Exit(1)
		}
		cfg.MaxConcurrent = n
	}

	cfg.KeeperServiceName = os.Getenv("CHUR_KEEPER_SERVICE_NAME")
	cfg.KeeperServiceNamespace = firstNonEmpty(os.Getenv("CHUR_KEEPER_SERVICE_NAMESPACE"), "chur-system")
	cfg.KeeperServicePort = firstNonEmpty(os.Getenv("CHUR_KEEPER_SERVICE_PORT"), "9443")
	cfg.KeeperTLSCertPath = os.Getenv("CHUR_KEEPER_TLS_CERT_PATH")
	cfg.KeeperTLSKeyPath = os.Getenv("CHUR_KEEPER_TLS_KEY_PATH")
	cfg.KeeperServerCA = os.Getenv("CHUR_KEEPER_SERVER_CA")
	cfg.KeeperClientCertSecretName = os.Getenv("CHUR_KEEPER_CLIENT_CERT_SECRET_NAME")

	maxSize, err := resource.ParseQuantity(cfg.MaxSecretSize)
	if err == nil && cfg.VolumeSizeLimit.Value() < maxSize.Value() {
		slog.ErrorContext(ctx, "CHUR_VOLUME_SIZE_LIMIT is smaller than CHUR_MAX_SECRET_SIZE",
			"volume_size_limit", cfg.VolumeSizeLimit.String(), "max_secret_size", cfg.MaxSecretSize)
		os.Exit(1)
	}

	srv, err := webhook.NewServer(cfg)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create webhook server", "error", err)
		os.Exit(1)
	}

	listenAddr := os.Getenv("CHUR_LISTEN")
	if listenAddr == "" {
		listenAddr = ":8443"
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.ErrorContext(ctx, "failed to listen", "addr", listenAddr, "error", err)
		os.Exit(1)
	}
	listener = netutil.LimitListener(listener, cfg.MaxConcurrent)
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
		slog.ErrorContext(ctx, "invalid CHUR_TLS_MODE: must be 'server' or 'mtls'", "value", v)
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
			slog.ErrorContext(ctx, "failed to read client CA", "path", caPath, "error", err)
			os.Exit(1)
		}
	}

	tlsCfg, err := webhook.TLSConfig(tlsMode, clientCAPEM)
	if err != nil {
		slog.ErrorContext(ctx, "failed to build TLS config", "error", err)
		os.Exit(1)
	}

	certFile := firstNonEmpty(os.Getenv("CHUR_TLS_CERT_PATH"), "/etc/chur/tls/tls.crt")
	keyFile := firstNonEmpty(os.Getenv("CHUR_TLS_KEY_PATH"), "/etc/chur/tls/tls.key")

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		if os.Getenv("CHUR_TLS_AUTO_GENERATE") == "1" {
			dnsName := os.Getenv("CHUR_TLS_DNS_NAME")
			if dnsName == "" {
				dnsName = "localhost"
			}
			slog.WarnContext(ctx, "TLS cert not found, generating self-signed certificate",
				"dns_name", dnsName, "path", certFile)
			tmpDir, err := os.MkdirTemp("", "chur-tls-*")
			if err != nil {
				slog.ErrorContext(ctx, "failed to create temp dir for dev cert", "error", err)
				os.Exit(1)
			}
			defer func() {
				if err := os.RemoveAll(tmpDir); err != nil {
					slog.ErrorContext(ctx, "failed to cleanup temp dir", "path", tmpDir, "error", err)
				}
			}()
			certFile = tmpDir + "/tls.crt"
			keyFile = tmpDir + "/tls.key"
			if err := tls.GenerateTLSCert(dnsName, certFile, keyFile); err != nil {
				slog.ErrorContext(ctx, "failed to generate dev TLS cert", "error", err)
				os.Exit(1)
			}
		} else {
			slog.ErrorContext(ctx, "TLS cert not found and CHUR_TLS_AUTO_GENERATE is not set",
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

	slog.InfoContext(ctx, "webhook configuration loaded",
		"version", version,
		"listen", listenAddr,
		"health_listen", healthAddr,
		"tls_mode", tlsMode,
		"volume_size_limit", cfg.VolumeSizeLimit.String(),
		"allowed_namespaces", cfg.AllowedNamespaces,
		"init_image", cfg.InitImage,
		"max_secret_size", cfg.MaxSecretSize,
		"local_base_path", cfg.LocalBasePath,
		"max_concurrent", cfg.MaxConcurrent,
	)

	srvErr := make(chan error, 2)

	go func() {
		slog.InfoContext(ctx, "starting chur-webhook admission",
			"version", version, "addr", listenAddr, "tls_mode", tlsMode)
		if err := httpSrv.ServeTLS(listener, certFile, keyFile); err != nil && err != http.ErrServerClosed {
			srvErr <- fmt.Errorf("admission server: %w", err)
			return
		}
	}()

	go func() {
		slog.InfoContext(ctx, "starting chur-webhook health", "addr", healthSrv.Addr)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- fmt.Errorf("health server: %w", err)
			return
		}
	}()

	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down...")
	case err := <-srvErr:
		slog.ErrorContext(ctx, "server error, shutting down", "error", err)
		cancel()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "health server shutdown error", "error", err)
	}
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "admission server shutdown error", "error", err)
	}

	// If any server error was captured, exit non-zero so K8s restarts the pod.
	for i := 0; i < 2; i++ {
		select {
		case err := <-srvErr:
			if err != nil {
				slog.ErrorContext(ctx, "exiting with error", "error", err)
				os.Exit(1)
			}
		default:
		}
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func validateDNS1123Label(s string) error {
	if len(s) == 0 || len(s) > 63 {
		return fmt.Errorf("must be 1-63 characters long")
	}
	for i, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return fmt.Errorf("invalid character %q at position %d", r, i)
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return fmt.Errorf("must not start or end with '-'")
	}
	return nil
}
