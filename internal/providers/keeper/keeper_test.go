package keeper

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetSecretSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/secrets/get" {
			t.Errorf("expected /v1/secrets/get, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("secret-value"))
	}))
	defer srv.Close()

	p, err := NewProvider(srv.URL, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.GetSecret(context.Background(), "test/ref")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-value" {
		t.Errorf("got %q, want %q", string(data), "secret-value")
	}
}

func TestGetSecretError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	p, err := NewProvider(srv.URL, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetSecret(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetSecretServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	p, err := NewProvider(srv.URL, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got %v", err)
	}
}

func TestNewProviderTLSConfig(t *testing.T) {
	t.Parallel()
	p, err := NewProvider("https://localhost:9443", false, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := p.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("expected TLS 1.3 min, got %v", tr.TLSClientConfig.MinVersion)
	}
}
