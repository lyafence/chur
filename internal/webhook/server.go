package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type Server struct {
	cfg          *Config
	deserializer runtime.Decoder
	sem          chan struct{}
	mutateFn     func(*admissionv1.AdmissionReview) *admissionv1.AdmissionReview
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
	_ = corev1.AddToScheme(scheme)
	_ = admissionv1.AddToScheme(scheme)

	s := &Server{
		cfg:          cfg,
		deserializer: serializer.NewCodecFactory(scheme).UniversalDeserializer(),
		sem:          make(chan struct{}, maxConcurrent),
	}
	s.mutateFn = s.mutate
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		slog.Warn("method not allowed", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit concurrent admission reviews to protect the webhook from
	// unbounded goroutine growth under load.
	select {
	case s.sem <- struct{}{}:
	case <-r.Context().Done():
		slog.Warn("admission review rejected, server busy or request canceled")
		http.Error(w, "server busy or request canceled", http.StatusServiceUnavailable)
		return
	}
	defer func() { <-s.sem }()

	slog.Info("admission review received", "path", r.URL.Path)

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		slog.Warn("failed to decode admission review", "error", err)
		http.Error(w, fmt.Sprintf("failed to decode: %v", err), http.StatusBadRequest)
		return
	}

	resp := s.mutateFn(&review)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode admission response", "error", err)
	}
}

func (s *Server) mutate(review *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
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
		slog.Warn("invalid request kind", "kind", kindString(review.Request.Kind))
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("unexpected resource kind: %s", kindString(review.Request.Kind)),
		}
		return resp
	}

	pod := &corev1.Pod{}
	if _, _, err := s.deserializer.Decode(review.Request.Object.Raw, nil, pod); err != nil {
		slog.Warn("failed to decode pod", "error", err)
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("failed to decode pod: %v", err),
		}
		return resp
	}

	patch, err := MutatePod(pod, s.cfg)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, ErrValidation) {
			code = http.StatusBadRequest
			slog.Warn("pod mutation validation failed",
				"pod", pod.Name, "namespace", pod.Namespace, "error", err)
		} else {
			slog.Error("failed to mutate pod",
				"pod", pod.Name, "namespace", pod.Namespace, "error", err)
		}
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    int32(code),
			Message: fmt.Sprintf("failed to mutate pod: %v", err),
		}
		return resp
	}

	// No mutation needed (pod lacks chur annotations).
	if patch == nil {
		resp.Response.Allowed = true
		return resp
	}

	providerName := pod.Annotations[annotationProvider]

	// Dry-run: return Allowed without patches. Webhooks must not actuate
	// side effects (init container creation, file writes) during dry-run.
	if review.Request.DryRun != nil && *review.Request.DryRun {
		slog.Info("dry-run request, skipping mutation",
			"pod", pod.Name, "namespace", pod.Namespace, "provider", providerName)
		resp.Response.Allowed = true
		return resp
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		slog.Error("failed to marshal patch", "error", err)
		resp.Response.Allowed = false
		resp.Response.Result = &metav1.Status{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("failed to marshal patch: %v", err),
		}
		return resp
	}

	slog.Info("pod mutated",
		"pod", pod.Name, "namespace", pod.Namespace, "provider", providerName)

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

// HealthHandler returns an HTTP handler with /healthz and /readyz endpoints.
func HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/readyz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
