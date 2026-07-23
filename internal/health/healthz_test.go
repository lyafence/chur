package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyafence/chur/internal/health"
)

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
