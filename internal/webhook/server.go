package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/lyafence/chur/internal/metrics"
)

type Server struct {
	cfg          *Config
	deserializer runtime.Decoder
	sem          chan struct{}
	mutateFn     func(context.Context, *admissionv1.AdmissionReview) *admissionv1.AdmissionReview
}

func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 100
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add corev1 to scheme: %w", err)
	}
	if err := admissionv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add admissionv1 to scheme: %w", err)
	}

	s := &Server{
		cfg:          cfg,
		deserializer: serializer.NewCodecFactory(scheme).UniversalDeserializer(),
		sem:          make(chan struct{}, maxConcurrent),
	}
	s.mutateFn = s.mutateWithMetrics
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		slog.WarnContext(r.Context(), "method not allowed", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit concurrent admission reviews to protect the webhook from
	// unbounded goroutine growth under load.
	select {
	case s.sem <- struct{}{}:
		metrics.WebhookConcurrentRequests.Inc()
	case <-r.Context().Done():
		slog.WarnContext(r.Context(), "admission review rejected, server busy or request canceled")
		metrics.WebhookAdmissionErrorsTotal.WithLabelValues("timeout").Inc()
		http.Error(w, "server busy or request canceled", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		<-s.sem
		metrics.WebhookConcurrentRequests.Dec()
	}()

	slog.InfoContext(r.Context(), "admission review received", "path", r.URL.Path)

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		slog.WarnContext(r.Context(), "failed to decode admission review", "error", err)
		http.Error(w, fmt.Sprintf("failed to decode: %v", err), http.StatusBadRequest)
		return
	}

	resp := s.mutateFn(r.Context(), &review)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode admission response", "error", err)
	}
}

func (s *Server) mutate(ctx context.Context, review *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	start := time.Now()
	resp := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	}

	if review.Request == nil {
		resp.Response = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "admission request is missing",
			},
		}
		return resp
	}

	resp.Response = &admissionv1.AdmissionResponse{UID: review.Request.UID}

	if !isPodRequest(review.Request.Kind) {
		slog.WarnContext(ctx, "invalid request kind", "kind", kindString(review.Request.Kind))
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("unexpected resource kind: %s", kindString(review.Request.Kind)),
		}
		return resp
	}

	pod := &corev1.Pod{}
	if _, _, err := s.deserializer.Decode(review.Request.Object.Raw, nil, pod); err != nil {
		slog.WarnContext(ctx, "failed to decode pod", "error", err)
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to decode pod: %v", err),
		}
		return resp
	}

	patch, ai, err := MutatePod(pod, s.cfg)

	// Audit log: build structured fields from AuditInfo and call context.
	if ai != nil {
		ai.RequestUID = review.Request.UID
	} else {
		ai = &AuditInfo{
			Namespace: pod.Namespace,
			Pod:       pod.Name,
		}
	}
	ai.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		reason := "internal"
		code := http.StatusInternalServerError
		if errors.Is(err, ErrValidation) {
			reason = "validation_error"
			code = http.StatusBadRequest
		}
		ai.Result = "error"
		metrics.WebhookAdmissionErrorsTotal.WithLabelValues(reason).Inc()
		metrics.WebhookAdmissionResultsTotal.WithLabelValues("error").Inc()
		slog.WarnContext(ctx, "audit",
			"request_uid", ai.RequestUID,
			"namespace", ai.Namespace,
			"pod", ai.Pod,
			"provider", ai.Provider,
			"duration_ms", ai.DurationMs,
			"result", ai.Result,
			"error", err,
		)
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    int32(code),
			Message: fmt.Sprintf("failed to mutate pod: %v", err),
		}
		return resp
	}

	// No mutation needed (pod lacks chur annotations).
	if patch == nil {
		ai.Result = "allowed"
		metrics.WebhookAdmissionResultsTotal.WithLabelValues("allowed").Inc()
		slog.InfoContext(ctx, "audit",
			"request_uid", ai.RequestUID,
			"namespace", ai.Namespace,
			"pod", ai.Pod,
			"duration_ms", ai.DurationMs,
			"result", ai.Result,
		)
		resp.Response.Allowed = true
		return resp
	}

	// Dry-run: return Allowed without patches. Webhooks must not actuate
	// side effects (init container creation, file writes) during dry-run.
	if review.Request.DryRun != nil && *review.Request.DryRun {
		ai.Result = "allowed"
		metrics.WebhookAdmissionResultsTotal.WithLabelValues("allowed").Inc()
		slog.InfoContext(ctx, "audit",
			"request_uid", ai.RequestUID,
			"namespace", ai.Namespace,
			"pod", ai.Pod,
			"provider", ai.Provider,
			"duration_ms", ai.DurationMs,
			"result", ai.Result,
		)
		resp.Response.Allowed = true
		return resp
	}

	metrics.WebhookProviderInjectionsTotal.WithLabelValues(ai.Provider).Inc()

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		ai.Result = "error"
		metrics.WebhookAdmissionErrorsTotal.WithLabelValues("internal").Inc()
		metrics.WebhookAdmissionResultsTotal.WithLabelValues("error").Inc()
		slog.WarnContext(ctx, "audit",
			"request_uid", ai.RequestUID,
			"namespace", ai.Namespace,
			"pod", ai.Pod,
			"provider", ai.Provider,
			"duration_ms", ai.DurationMs,
			"result", ai.Result,
			"error", err,
		)
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("failed to marshal patch: %v", err),
		}
		return resp
	}

	ai.Result = "mutated"
	metrics.WebhookAdmissionResultsTotal.WithLabelValues("mutated").Inc()
	slog.InfoContext(ctx, "audit",
		"request_uid", ai.RequestUID,
		"namespace", ai.Namespace,
		"pod", ai.Pod,
		"provider", ai.Provider,
		"duration_ms", ai.DurationMs,
		"result", ai.Result,
	)

	pt := admissionv1.PatchTypeJSONPatch
	resp.Response.Allowed = true
	resp.Response.Patch = patchBytes
	resp.Response.PatchType = &pt
	return resp
}

func isPodRequest(kind metav1.GroupVersionKind) bool {
	return kind.Group == "" && kind.Version == "v1" && kind.Kind == "Pod"
}

func kindString(kind metav1.GroupVersionKind) string {
	return fmt.Sprintf("%s/%s %s", kind.Group, kind.Version, kind.Kind)
}

// mutateWithMetrics wraps mutate with Prometheus metric recording.
func (s *Server) mutateWithMetrics(ctx context.Context, review *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	start := time.Now()
	resp := s.mutate(ctx, review)

	allowed := resp.Response != nil && resp.Response.Allowed
	mutated := resp.Response != nil && resp.Response.PatchType != nil
	metrics.WebhookAdmissionRequestsTotal.
		WithLabelValues(fmt.Sprintf("%t", allowed), fmt.Sprintf("%t", mutated)).
		Inc()

	metrics.WebhookAdmissionDurationSeconds.Observe(time.Since(start).Seconds())

	return resp
}

// HealthHandler returns an HTTP handler with /healthz, /readyz, and /metrics endpoints.
func HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/readyz", healthz)
	mux.Handle("/metrics", metrics.Handler())
	return mux
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		slog.WarnContext(r.Context(), "health: failed to write response", "error", err)
	}
}
