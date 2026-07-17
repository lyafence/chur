package tls

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateCertMemory(t *testing.T) {
	t.Parallel()
	certPEM, keyPEM, err := GenerateCertMemory("localhost")
	if err != nil {
		t.Fatal(err)
	}
	if len(certPEM) == 0 {
		t.Error("cert PEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Error("key PEM is empty")
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("expected CERTIFICATE block, got %s", block.Type)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.PublicKeyAlgorithm != x509.ECDSA {
		t.Errorf("expected ECDSA key, got %v", cert.PublicKeyAlgorithm)
	}
	if _, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Error("expected ecdsa.PublicKey")
	}
	if time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
		t.Error("cert validity period does not include current time")
	}
	if cert.NotAfter.Sub(cert.NotBefore) > 366*24*time.Hour {
		t.Errorf("cert validity too long: %v", cert.NotAfter.Sub(cert.NotBefore))
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode key PEM")
	}
	if keyBlock.Type != "EC PRIVATE KEY" {
		t.Errorf("expected EC PRIVATE KEY block, got %s", keyBlock.Type)
	}
	_, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenerateCertMemory_IPAddress(t *testing.T) {
	t.Parallel()
	certPEM, _, err := GenerateCertMemory("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.DNSNames) > 0 {
		t.Error("expected DNSNames to be empty for IP")
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %v", cert.IPAddresses)
	}
}

func TestClientCAPool_ValidPEM(t *testing.T) {
	t.Parallel()
	certPEM, _, err := GenerateCertMemory("localhost")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := ClientCAPool(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestClientCAPool_InvalidPEM(t *testing.T) {
	t.Parallel()
	_, err := ClientCAPool([]byte("not-a-cert"))
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestClientCAPool_EmptyInput(t *testing.T) {
	t.Parallel()
	_, err := ClientCAPool([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestServerTLSConfig_SelfSigned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	if err := GenerateTLSCert("localhost", certFile, keyFile); err != nil {
		t.Fatal(err)
	}

	cfg, err := ServerTLSConfig(nil, certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion == 0 {
		t.Error("expected non-zero MinVersion")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
	if cfg.ClientAuth != 0 {
		t.Errorf("expected ClientAuth=0 for self-signed, got %v", cfg.ClientAuth)
	}
}

func TestServerTLSConfig_MissingCert(t *testing.T) {
	t.Parallel()
	_, err := ServerTLSConfig(nil, "/nonexistent/cert", "/nonexistent/key")
	if err == nil {
		t.Error("expected error for missing cert")
	}
}

func TestServerTLSConfig_MTLS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	if err := GenerateTLSCert("localhost", certFile, keyFile); err != nil {
		t.Fatal(err)
	}
	caPEM, _, err := GenerateCertMemory("ca")
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := ServerTLSConfig(caPEM, certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientAuth == 0 {
		t.Error("expected ClientAuth for mTLS, got 0")
	}
	if cfg.ClientCAs == nil {
		t.Error("expected non-nil ClientCAs")
	}
}

func TestGenerateTLSCert(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")

	if err := GenerateTLSCert("example.com", certFile, keyFile); err != nil {
		t.Fatal(err)
	}

	certData, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(certData) == 0 {
		t.Error("cert file is empty")
	}

	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyData) == 0 {
		t.Error("key file is empty")
	}

	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm()&0077 != 0 {
		t.Error("key file has excessive permissions")
	}
}
