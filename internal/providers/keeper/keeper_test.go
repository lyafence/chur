package keeper

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func TestGetSecretContextTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		_, _ = w.Write([]byte("too-late"))
	}))
	defer srv.Close()

	p, err := NewProvider(srv.URL, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = p.GetSecret(ctx, "ref")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGetSecretResponseExceedsMaxSize(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2*1024*1024))
	}))
	defer srv.Close()

	p, err := NewProvider(srv.URL, true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.maxSecretSize = 1024
	_, err = p.GetSecret(context.Background(), "ref")
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("expected max size error, got %v", err)
	}
}

func TestGetSecretConnectionRefused(t *testing.T) {
	t.Parallel()
	p, err := NewProvider("https://127.0.0.1:1", true, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetSecret(context.Background(), "ref")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestGetSecretWithClientCert(t *testing.T) {
	t.Parallel()

	caCertPEM, caKeyPEM, err := generateTestCA()
	if err != nil {
		t.Fatal(err)
	}
	serverCertPEM, serverKeyPEM, err := signCert(caCertPEM, caKeyPEM, "server", true)
	if err != nil {
		t.Fatal(err)
	}
	clientCertPEM, clientKeyPEM, err := signCert(caCertPEM, caKeyPEM, "client", false)
	if err != nil {
		t.Fatal(err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("failed to parse CA cert")
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			t.Error("expected client certificate")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("mtls-secret"))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS13,
	}
	srv.StartTLS()
	defer srv.Close()

	dir := t.TempDir()
	certFile := dir + "/client.crt"
	keyFile := dir + "/client.key"
	caFile := dir + "/ca.crt"
	if err := os.WriteFile(certFile, clientCertPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, clientKeyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caFile, caCertPEM, 0644); err != nil {
		t.Fatal(err)
	}

	p, err := NewProvider(srv.URL, false, certFile, keyFile, caFile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.GetSecret(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mtls-secret" {
		t.Errorf("got %q, want %q", string(data), "mtls-secret")
	}
}

func generateTestCA() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM, nil
}

func signCert(caCertPEM, caKeyPEM []byte, commonName string, isServer bool) (certPEM, keyPEM []byte, err error) {
	caCertBlock, _ := pem.Decode(caCertPEM)
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if isServer {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames = []string{commonName}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM, nil
}
