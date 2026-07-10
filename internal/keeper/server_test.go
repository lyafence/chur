package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	h := handleGetSecret(b, 1<<20)

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
	b := &mockBackend{data: nil, err: &backendError{msg: "not found", code: http.StatusNotFound}}
	h := handleGetSecret(b, 1<<20)

	body, _ := json.Marshal(map[string]string{"ref": "missing"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
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
	h := handleGetSecret(b, 1<<20)

	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader([]byte("{bad")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetSecretOverMaxSize(t *testing.T) {
	t.Parallel()
	b := &mockBackend{data: []byte("too-big")}
	h := handleGetSecret(b, 5)

	body, _ := json.Marshal(map[string]string{"ref": "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/get", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}
