package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func testBackend(t *testing.T, srv *httptest.Server, tokenFile string, timeout time.Duration) *HTTPBackend {
	t.Helper()
	b, err := New(srv.URL, tokenFile, timeout, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	b.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	return b
}

func TestHTTPBackendSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("ref") != "prod/db-password" {
			t.Errorf("expected ref=prod/db-password, got %s", r.URL.Query().Get("ref"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte("secret-value"))
	}))
	defer srv.Close()

	tf, _ := os.CreateTemp(t.TempDir(), "token")
	if _, err := tf.WriteString("test-token"); err != nil {
		t.Fatal(err)
	}
	tf.Close()

	b := testBackend(t, srv, tf.Name(), 0)
	data, err := b.GetSecret(context.Background(), "prod/db-password")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-value" {
		t.Errorf("got %q, want %q", string(data), "secret-value")
	}
}

func TestHTTPBackendNoAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 0)
	_, err := b.GetSecret(context.Background(), "ref")
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPBackend404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 0)
	_, err := b.GetSecret(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestHTTPBackend5xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 0)
	_, err := b.GetSecret(context.Background(), "fail")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestHTTPBackendContextCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("should not arrive"))
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 0)
	_, err := b.GetSecret(ctx, "ref")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHTTPBackendInvalidRef(t *testing.T) {
	t.Parallel()
	b, err := New("https://example.com", "", 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.GetSecret(context.Background(), "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestHTTPBackendEmptyURL(t *testing.T) {
	t.Parallel()
	_, err := New("", "", 0, 1<<20)
	if err == nil {
		t.Fatal("expected error for empty url")
	}
}

func TestHTTPBackendMissingTokenFile(t *testing.T) {
	t.Parallel()
	_, err := New("https://example.com", "/nonexistent/token", 0, 1<<20)
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
}

func TestHTTPBackendQueryEncode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("ref")
		if ref != "prod/db-password" {
			t.Errorf("expected 'prod/db-password', got %q", ref)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 0)
	if _, err := b.GetSecret(context.Background(), "prod/db-password"); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPBackendExceedsMaxSize(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("this-is-too-big"))
	}))
	defer srv.Close()

	b, err := New(srv.URL, "", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	b.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	_, err = b.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
}

func TestHTTPBackendTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // wait for client timeout
		_, _ = w.Write([]byte("too-late"))
	}))
	defer srv.Close()

	b := testBackend(t, srv, "", 50*time.Millisecond)
	_, err := b.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHTTPBackendRejectsHTTP(t *testing.T) {
	t.Parallel()
	_, err := New("http://example.com", "", 0, 1<<20)
	if err == nil {
		t.Fatal("expected error for http scheme")
	}
}

func TestHTTPBackendRejectsNoScheme(t *testing.T) {
	t.Parallel()
	_, err := New("example.com", "", 0, 1<<20)
	if err == nil {
		t.Fatal("expected error for missing scheme")
	}
}
