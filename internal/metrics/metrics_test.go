package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	h := Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if body == "" {
			t.Error("expected non-empty metrics body")
		}
		if !strings.Contains(body, "chur_") {
			t.Error("expected chur_ metrics in response body")
		}
	})

}
