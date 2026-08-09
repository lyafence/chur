package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/netutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	"github.com/lyafence/chur/internal/bytesize"
	"github.com/lyafence/chur/internal/health"
	"github.com/lyafence/chur/internal/metrics"
	"github.com/lyafence/chur/internal/tls"
	"github.com/lyafence/chur/internal/validate"
	"github.com/lyafence/chur/internal/webhook"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := webhook.DefaultConfig()

	if v := os.Getenv("CHUR_VOLUME_SIZE_LIMIT"); v != "" {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_VOLUME_SIZE_LIMIT", "value", v, "error", err)
			return 1
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
				return 1
			}
			cfg.AllowedNamespaces = append(cfg.AllowedNamespaces, ns)
		}
	}

	if v := os.Getenv("CHUR_INIT_IMAGE"); v != "" {
		cfg.InitImage = v
	} else if version != "dev" {
		cfg.InitImage = "ghcr.io/lyafence/chur-init:" + version
	}
	if v := os.Getenv("CHUR_INIT_IMAGE_PULL_POLICY"); v != "" {
		switch corev1.PullPolicy(v) {
		case corev1.PullAlways, corev1.PullNever, corev1.PullIfNotPresent:
			cfg.InitImagePullPolicy = corev1.PullPolicy(v)
		default:
			slog.ErrorContext(ctx, "invalid CHUR_INIT_IMAGE_PULL_POLICY: must be Always, Never, or IfNotPresent", "value", v)
			return 1
		}
	}
	if v := os.Getenv("CHUR_RUN_AS_USER"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_RUN_AS_USER", "value", v, "error", err)
			return 1
		}
		cfg.RunAsUser = n
	}
	if v := os.Getenv("CHUR_RUN_AS_GROUP"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_RUN_AS_GROUP", "value", v, "error", err)
			return 1
		}
		cfg.RunAsGroup = ptr.To(n)
	}
	if v := os.Getenv("CHUR_FS_GROUP"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_FS_GROUP", "value", v, "error", err)
			return 1
		}
		cfg.FSGroup = n
	}
	if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
		parsed, err := parseMaxSecretSizeEnv(v, cfg.VolumeSizeLimit)
		if err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_MAX_SECRET_SIZE", "value", v, "error", err)
			return 1
		}
		cfg.MaxSecretSize = parsed
	}
	if _, err := parseMaxSecretSizeEnv(cfg.MaxSecretSize, cfg.VolumeSizeLimit); err != nil {
		slog.ErrorContext(ctx, "invalid CHUR_MAX_SECRET_SIZE", "value", cfg.MaxSecretSize, "error", err)
		return 1
	}
	if v := os.Getenv("CHUR_LOCAL_BASE_PATH"); v != "" {
		if err := validate.ValidateLocalBasePath(v); err != nil {
			slog.ErrorContext(ctx, "invalid CHUR_LOCAL_BASE_PATH", "value", v, "error", err)
			return 1
		}
		cfg.LocalBasePath = v
	}
	if v := os.Getenv("CHUR_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			slog.ErrorContext(ctx, "invalid CHUR_MAX_CONCURRENT", "value", v, "error", err)
			return 1
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
	cfg.KeeperClientMaxSecretSize = os.Getenv("CHUR_KEEPER_CLIENT_MAX_SECRET_SIZE")
	if v := os.Getenv("CHUR_ALLOW_KEEPER_SKIP_VERIFY"); v == "1" || v == "true" {
		cfg.AllowKeeperSkipVerify = true
	}

	srv, err := webhook.NewServer(cfg)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create webhook server", "error", err)
		return 1
	}

	listenAddr := os.Getenv("CHUR_LISTEN")
	if listenAddr == "" {
		listenAddr = ":8443"
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.ErrorContext(ctx, "failed to listen", "addr", listenAddr, "error", err)
		return 1
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
		return 1
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
			return 1
		}
	}

	tlsCfg, err := webhook.TLSConfig(tlsMode, clientCAPEM)
	if err != nil {
		slog.ErrorContext(ctx, "failed to build TLS config", "error", err)
		return 1
	}

	certFile := firstNonEmpty(os.Getenv("CHUR_TLS_CERT_PATH"), "/etc/chur/tls/tls.crt")
	keyFile := firstNonEmpty(os.Getenv("CHUR_TLS_KEY_PATH"), "/etc/chur/tls/tls.key")

	if _, err := os.Stat(certFile); errors.Is(err, fs.ErrNotExist) {
		if os.Getenv("CHUR_TLS_AUTO_GENERATE") == "1" {
			dnsName := os.Getenv("CHUR_TLS_DNS_NAME")
			if dnsName == "" {
				dnsName = "localhost"
			}
			slog.WarnContext(ctx, "TLS cert not found, generating self-signed certificate",
				"dns_name", dnsName, "path", certFile)
			var cleanup func() error
			certFile, keyFile, cleanup, err = tls.GenerateTempCert(dnsName)
			if err != nil {
				slog.ErrorContext(ctx, "failed to generate dev TLS cert", "error", err)
				return 1
			}
			defer func() {
				if err := cleanup(); err != nil {
					slog.ErrorContext(ctx, "failed to cleanup temp dir", "error", err)
				}
			}()
		} else {
			slog.ErrorContext(ctx, "TLS cert not found and CHUR_TLS_AUTO_GENERATE is not set",
				"path", certFile, "hint", "mount certs to /etc/chur/tls or set CHUR_TLS_AUTO_GENERATE=1")
			return 1
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

	healthMux := http.NewServeMux()
	health.RegisterHealthEndpoints(healthMux, "webhook", metrics.Handler())
	healthSrv := &http.Server{
		Addr:              healthAddr,
		Handler:           healthMux,
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

	var (
		errMu    sync.Mutex
		firstErr error
	)
	errCh := make(chan struct{}, 1)
	getErr := func() error {
		errMu.Lock()
		defer errMu.Unlock()
		return firstErr
	}
	notifyErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
			errCh <- struct{}{}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		slog.InfoContext(ctx, "starting chur-webhook admission",
			"version", version, "addr", listenAddr, "tls_mode", tlsMode)
		if err := httpSrv.ServeTLS(listener, certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			notifyErr(fmt.Errorf("admission server: %w", err))
			return
		}
	}()

	go func() {
		defer wg.Done()
		slog.InfoContext(ctx, "starting chur-webhook health", "addr", healthSrv.Addr)
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			notifyErr(fmt.Errorf("health server: %w", err))
			return
		}
	}()

	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down...")
	case <-errCh:
		slog.ErrorContext(ctx, "server error, shutting down", "error", getErr())
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

	// Both server goroutines have returned; any captured error is final.
	wg.Wait()
	if err := getErr(); err != nil {
		slog.ErrorContext(ctx, "exiting with error", "error", err)
		return 1
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseMaxSecretSizeEnv validates CHUR_MAX_SECRET_SIZE with the same parser
// chur-init uses (internal/bytesize), so admission never accepts a value
// the init container cannot parse. Returns the value unchanged, or an error.
func parseMaxSecretSizeEnv(v string, volumeLimit resource.Quantity) (string, error) {
	if v == "" {
		return "", nil
	}
	maxBytes, err := bytesize.Parse(v)
	if err != nil {
		return "", fmt.Errorf("invalid CHUR_MAX_SECRET_SIZE %q: %w", v, err)
	}
	if volumeLimit.Value() < maxBytes {
		return "", fmt.Errorf("CHUR_VOLUME_SIZE_LIMIT %s is smaller than CHUR_MAX_SECRET_SIZE %q", volumeLimit.String(), v)
	}
	return v, nil
}

func validateDNS1123Label(s string) error {
	if errs := validation.IsDNS1123Label(s); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
