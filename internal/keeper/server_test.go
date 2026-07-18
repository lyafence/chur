package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyafence/chur/internal/metrics"
)

type mockBackend struct {
	data []byte
	err  error
}

func (m *mockBackend) Name() string { return "mock" }
func (m *mockBackend) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return m.data, m.err
}

func TestHandleGetSecretSuccess(t *testing.T) {
	t.Parallel()
	b := &mockBackend{data: []byte("secret-value")}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	body, _ := json.Marshal(map[string]string{"ref": "test/secret"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); string(got) != "secret-value" {
		t.Errorf("got %q, want %q", string(got), "secret-value")
	}
}

func TestHandleGetSecretNotFound(t *testing.T) {
	t.Parallel()
	b := &mockBackend{data: nil, err: fmt.Errorf("not found")}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	body, _ := json.Marshal(map[string]string{"ref": "missing"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestHandleGetSecretBadJSON(t *testing.T) {
	t.Parallel()
	b := &mockBackend{}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetSecretWrongMethod(t *testing.T) {
	t.Parallel()
	b := &mockBackend{}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	req := httptest.NewRequest(http.MethodGet, "/v1/secrets/get", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleGetSecretWrongPath(t *testing.T) {
	t.Parallel()
	b := &mockBackend{}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/put", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

type blockingBackend struct {
	name    string
	blocked chan struct{}
	release chan struct{}
}

func (b *blockingBackend) Name() string { return b.name }

func (b *blockingBackend) GetSecret(_ context.Context, _ string) ([]byte, error) {
	close(b.blocked)
	<-b.release
	return nil, fmt.Errorf("released")
}

func TestHandleGetSecretConcurrencyLimit(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	release := make(chan struct{})
	sem := make(chan struct{}, 1)

	bb := &blockingBackend{name: "blocking", blocked: blocked, release: release}
	h := handleGetSecret(bb, 1<<20, sem, bb.Name())

	go func() {
		body, _ := json.Marshal(map[string]string{"ref": "x"})
		req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}()

	<-blocked

	body, _ := json.Marshal(map[string]string{"ref": "y"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when busy, got %d", rec.Code)
	}
	close(release)
}

func TestHandleGetSecretOverMaxSize(t *testing.T) {
	t.Parallel()
	b := &mockBackend{data: []byte("too-big")}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 5, sem, b.Name())

	body, _ := json.Marshal(map[string]string{"ref": "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestKeeperMetrics(t *testing.T) {
	t.Parallel()
	b := &mockBackend{data: []byte("secret-value")}
	sem := make(chan struct{}, 100)
	h := handleGetSecret(b, 1<<20, sem, b.Name())

	body, _ := json.Marshal(map[string]string{"ref": "test/ref"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	mh := metrics.Handler()
	mreq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mrec := httptest.NewRecorder()
	mh.ServeHTTP(mrec, mreq)

	if mrec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", mrec.Code)
	}
	bodyStr := mrec.Body.String()
	if !strings.Contains(bodyStr, "chur_keeper_requests_total") {
		t.Error("expected chur_keeper_requests_total in metrics output")
	}
	if !strings.Contains(bodyStr, "chur_keeper_request_duration_seconds") {
		t.Error("expected chur_keeper_request_duration_seconds in metrics output")
	}
}
