package keeper

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lyafence/chur/internal/metrics"
	churtls "github.com/lyafence/chur/internal/tls"
	"github.com/lyafence/chur/internal/validate"
)

type getRequest struct {
	Ref string `json:"ref"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Serve starts the keeper HTTPS server and blocks until shutdown.
// The caller is responsible for creating and closing the listener.
func Serve(ctx context.Context, cfg *Config, tlsCfg *tls.Config, listener net.Listener) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	sem := make(chan struct{}, cfg.MaxConcurrent)
	backendName := cfg.Backend.Name()
	mux.HandleFunc("/v1/secrets/get", handleGetSecret(cfg.Backend, cfg.MaxSecretSize, sem, backendName).ServeHTTP)

	srv := &http.Server{
		Handler:           mux,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	srvErr := make(chan error, 2)

	var healthSrv *http.Server
	if cfg.HealthListen != "" {
		healthMux := http.NewServeMux()
		healthMux.HandleFunc("/healthz", healthz)
		healthMux.Handle("/metrics", metrics.Handler())
		healthSrv = &http.Server{
			Addr:              cfg.HealthListen,
			Handler:           healthMux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		go func() {
			slog.InfoContext(ctx, "keeper health server starting", "addr", cfg.HealthListen)
			if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.ErrorContext(ctx, "keeper health server error", "error", err)
				srvErr <- err
				return
			}
		}()
	}
	go func() {
		slog.InfoContext(ctx, "keeper server starting",
			"addr", listener.Addr().String(),
			"tls_mode", cfg.TLSMode,
			"backend", cfg.Backend.Name(),
		)
		if err := srv.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "keeper server error", "error", err)
			srvErr <- err
			return
		}
	}()

	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "keeper shutting down...")
	case err := <-srvErr:
		slog.ErrorContext(ctx, "keeper server failed, shutting down", "error", err)
		cancel()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if healthSrv != nil {
		if err := healthSrv.Shutdown(shutdownCtx); err != nil {
			slog.ErrorContext(ctx, "keeper health server shutdown error", "error", err)
		}
	}
	shutdownErr := srv.Shutdown(shutdownCtx)

	for i := 0; i < 2; i++ {
		select {
		case err := <-srvErr:
			if err != nil {
				return err
			}
		default:
		}
	}
	return shutdownErr
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		slog.WarnContext(r.Context(), "keeper health: failed to write response", "error", err)
	}
}

func handleGetSecret(b Backend, maxSize int64, sem chan struct{}, backendName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if r.Method != http.MethodPost {
			writeError(r.Context(), w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/secrets/get" {
			writeError(r.Context(), w, "not found", http.StatusNotFound)
			return
		}

		select {
		case sem <- struct{}{}:
			metrics.KeeperConcurrentRequests.Inc()
		case <-r.Context().Done():
			writeError(r.Context(), w, "server busy", http.StatusServiceUnavailable)
			return
		}
		defer func() {
			<-sem
			metrics.KeeperConcurrentRequests.Dec()
		}()

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var req getRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			recordKeeperMetric(backendName, "error", start)
			writeError(r.Context(), w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Ref == "" {
			recordKeeperMetric(backendName, "error", start)
			writeError(r.Context(), w, "ref is required", http.StatusBadRequest)
			return
		}
		if err := validate.ValidateKeeperRef(req.Ref); err != nil {
			recordKeeperMetric(backendName, "error", start)
			writeError(r.Context(), w, "invalid ref", http.StatusBadRequest)
			return
		}

		data, err := b.GetSecret(r.Context(), req.Ref)
		durationMs := time.Since(start).Milliseconds()

		if err != nil {
			slog.WarnContext(r.Context(), "keeper: backend get failed",
				"ref", req.Ref,
				"backend", backendName,
				"duration_ms", durationMs,
				"result", "error",
				"error", err,
			)
			recordKeeperMetric(backendName, "error", start)
			writeError(r.Context(), w, "internal server error", http.StatusInternalServerError)
			return
		}

		if int64(len(data)) > maxSize {
			slog.WarnContext(r.Context(), "keeper: secret exceeds max size",
				"ref", req.Ref,
				"backend", backendName,
				"duration_ms", durationMs,
				"result", "error",
				"size", len(data),
			)
			recordKeeperMetric(backendName, "error", start)
			writeError(r.Context(), w, "secret exceeds max size", http.StatusRequestEntityTooLarge)
			return
		}

		slog.InfoContext(r.Context(), "keeper: secret fetched",
			"ref", req.Ref,
			"backend", backendName,
			"duration_ms", durationMs,
			"result", "ok",
			"size", len(data),
		)
		recordKeeperMetric(backendName, "ok", start)

		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(data); err != nil {
			slog.ErrorContext(r.Context(), "keeper: failed to write response", "error", err)
		}
	}
}

func recordKeeperMetric(backend, status string, start time.Time) {
	metrics.KeeperRequestsTotal.WithLabelValues(backend, status).Inc()
	metrics.KeeperRequestDurationSeconds.WithLabelValues(backend).Observe(time.Since(start).Seconds())
}

func writeError(ctx context.Context, w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: msg}); err != nil {
		slog.ErrorContext(ctx, "keeper: failed to write error response", "error", err)
	}
}

// ServerTLSConfig builds a *tls.Config for keeper. For self-signed mode with
// no cert files provided, it generates a temporary certificate and returns a
// cleanup function that removes the temporary directory. The caller must call
// cleanup.
func ServerTLSConfig(_ context.Context, cfg *Config) (*tls.Config, func() error, error) {
	switch cfg.TLSMode {
	case TLSModeSelfSigned:
		certFile := cfg.TLSCertFile
		keyFile := cfg.TLSKeyFile
		cleanup := func() error { return nil }

		if certFile == "" || keyFile == "" {
			dnsName := os.Getenv("CHUR_KEEPER_TLS_DNS_NAME")
			if dnsName == "" {
				dnsName = "localhost"
			}
			tmpDir, err := os.MkdirTemp("", "chur-keeper-tls-*")
			if err != nil {
				return nil, nil, fmt.Errorf("keeper: temp dir: %w", err)
			}
			cleanup = func() error { return os.RemoveAll(tmpDir) }
			certFile = tmpDir + "/tls.crt"
			keyFile = tmpDir + "/tls.key"
			if err := churtls.GenerateTLSCert(dnsName, certFile, keyFile); err != nil {
				if cleanErr := cleanup(); cleanErr != nil {
					slog.WarnContext(context.Background(), "keeper: failed to clean up temp dir after cert generation failure", "error", cleanErr)
				}
				return nil, nil, fmt.Errorf("keeper: generate cert: %w", err)
			}
		}

		tlsCfg, err := churtls.ServerTLSConfig(nil, certFile, keyFile)
		if err != nil {
			if cleanErr := cleanup(); cleanErr != nil {
				slog.WarnContext(context.Background(), "keeper: failed to clean up temp dir after TLS config failure", "error", cleanErr)
			}
			return nil, nil, err
		}
		return tlsCfg, cleanup, nil

	case TLSModeMTLS:
		if cfg.ClientCAFile == "" {
			return nil, nil, fmt.Errorf("keeper: mtls mode requires CHUR_KEEPER_CLIENT_CA_PATH")
		}
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			return nil, nil, fmt.Errorf("keeper: mtls mode requires CHUR_KEEPER_TLS_CERT_PATH and CHUR_KEEPER_TLS_KEY_PATH")
		}
		clientCACert, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, nil, fmt.Errorf("keeper: read client CA: %w", err)
		}
		tlsCfg, err := churtls.ServerTLSConfig(clientCACert, cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, nil, err
		}
		return tlsCfg, func() error { return nil }, nil

	default:
		return nil, nil, fmt.Errorf("keeper: unknown tls mode: %s", cfg.TLSMode)
	}
}
