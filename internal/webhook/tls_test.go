package webhook

import (
	"crypto/tls"
	"encoding/pem"
	"testing"

	churtls "github.com/lyafence/chur/internal/tls"
)

func TestTLSConfig_Server_AllowsNoClientCert(t *testing.T) {
	t.Parallel()
	cfg, err := TLSConfig(TLSModeServer, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Fatalf("expected NoClientCert, got %v", cfg.ClientAuth)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3, got %v", cfg.MinVersion)
	}
}

func TestTLSConfig_MTLS_RequiresClientCert(t *testing.T) {
	t.Parallel()
	caPEM, _, err := churtls.GenerateCertMemory("test-ca")
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}

	cfg, err := TLSConfig(TLSModeMTLS, caPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected RequireAndVerifyClientCert, got %v", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("expected non-nil ClientCAs pool")
	}
}

func TestTLSConfig_MTLS_WithoutCA_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := TLSConfig(TLSModeMTLS, nil)
	if err == nil {
		t.Fatal("expected error for mtls with no CA")
	}

	_, err = TLSConfig(TLSModeMTLS, []byte("invalid-pem"))
	if err == nil {
		t.Fatal("expected error for mtls with invalid CA PEM")
	}
}

func TestTLSConfig_MTLS_WithInvalidCAPEM_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := TLSConfig(TLSModeMTLS, []byte("not-a-pem"))
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestTLSConfig_MTLS_ClientCAPoolNotEmpty(t *testing.T) {
	t.Parallel()
	caPEM, _, err := churtls.GenerateCertMemory("test-ca")
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}

	cfg, err := TLSConfig(TLSModeMTLS, caPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(caPEM)
	if block == nil {
		t.Fatal("failed to decode CA PEM")
	}

	if cfg.ClientCAs == nil {
		t.Fatal("expected non-nil ClientCAs pool")
	}
}

func TestTLSConfig_Server_CAPEMIgnored(t *testing.T) {
	t.Parallel()
	caPEM, _, err := churtls.GenerateCertMemory("test-ca")
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}

	cfg, err := TLSConfig(TLSModeServer, caPEM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Fatalf("expected NoClientCert in server mode")
	}
	if cfg.ClientCAs != nil {
		t.Fatal("expected nil ClientCAs in server mode")
	}
}

func TestTLSConfig_UnknownMode_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := TLSConfig(TLSMode("unknown"), nil)
	if err == nil {
		t.Fatal("expected error for unknown TLS mode")
	}
}
