package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyafence/chur/internal/health"
)

func TestRegisterHealthEndpoints_Standard(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	health.RegisterHealthEndpoints(mux, "test", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, ep := range []string{"/healthz", "/readyz", "/metrics"} {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", ep, rec.Code)
		}
	}
}

func TestRegisterHealthEndpoints_Subset(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	health.RegisterHealthEndpoints(mux, "test", nil, "/healthz")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz: expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("/readyz: expected 404 (not registered), got %d", rec.Code)
	}
}

func TestRegisterHealthEndpoints_UnknownEndpoint(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	health.RegisterHealthEndpoints(mux, "test", nil, "/unknown")

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown endpoint, got %d", rec.Code)
	}
}

func TestHealthzHandler(t *testing.T) {
	t.Parallel()
	h := health.HealthzHandler("test")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("expected body %q, got %q", `{"status":"ok"}`, rec.Body.String())
	}
}

func TestHealthzHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := health.HealthzHandler("test")
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/healthz", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for %s, got %d", method, rec.Code)
		}
	}
}
