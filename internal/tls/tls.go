package tls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// GenerateCertMemory generates a self-signed TLS certificate and key for the
// given host (DNS name or IP address). Both are returned as PEM-encoded byte
// slices. The cert is valid for 365 days.
func GenerateCertMemory(host string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}

	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return nil, nil, fmt.Errorf("encode cert PEM: %w", err)
	}

	var keyBuf bytes.Buffer
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal EC key: %w", err)
	}
	if err := pem.Encode(&keyBuf, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}); err != nil {
		return nil, nil, fmt.Errorf("encode key PEM: %w", err)
	}

	return certBuf.Bytes(), keyBuf.Bytes(), nil
}

// GenerateTLSCert creates a self-signed TLS cert and key for development and
// writes them to the given paths. The cert is valid for 365 days.
func GenerateTLSCert(host string, certPath, keyPath string) error {
	certPEM, keyPEM, err := GenerateCertMemory(host)
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write cert file: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}

	return nil
}

// ClientCAPool parses a PEM-encoded CA certificate and returns a *x509.CertPool
// suitable for verifying client certificates in TLS configs.
func ClientCAPool(pem []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("failed to parse client CA certificate")
	}
	return pool, nil
}

// ServerTLSConfig returns a *tls.Config loaded with a server certificate and
// optional client certificate verification. clientCAPEM may be nil for self-signed mode.
func ServerTLSConfig(clientCAPEM []byte, certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	if len(clientCAPEM) > 0 {
		pool, err := ClientCAPool(clientCAPEM)
		if err != nil {
			return nil, err
		}
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = pool
	}

	return cfg, nil
}
