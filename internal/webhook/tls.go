package webhook

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
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

// TLSMode controls whether the webhook requires client certificates.
type TLSMode string

const (
	TLSModeDev  TLSMode = "dev"  // NoClientCert — development only
	TLSModeProd TLSMode = "prod" // RequireAndVerifyClientCert — mTLS
)

// TLSConfig returns a *tls.Config for the webhook's HTTPS server.
// In prod mode, the API server must present a valid client certificate.
func TLSConfig(mode TLSMode) *tls.Config {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	if mode == TLSModeProd {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg
}

// GenerateCertMemory generates a self-signed TLS certificate and key for the
// given host (DNS name or IP address). Both are returned as PEM-encoded byte
// slices. The cert is valid for 365 days.
func GenerateCertMemory(host string) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
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
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
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
	if err := pem.Encode(&keyBuf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
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
