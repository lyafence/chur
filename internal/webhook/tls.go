package webhook

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// TLSMode controls whether the webhook requires client certificates.
type TLSMode string

const (
	TLSModeServer TLSMode = "server" // NoClientCert — default mode
	TLSModeMTLS   TLSMode = "mtls"   // RequireAndVerifyClientCert — mTLS
)

// TLSConfig returns a *tls.Config for the webhook's HTTPS server.
// In mtls mode, the API server must present a valid client certificate
// signed by the CA provided in clientCAPEM.
func TLSConfig(mode TLSMode, clientCAPEM []byte) (*tls.Config, error) {
	switch mode {
	case TLSModeServer:
		return &tls.Config{
			MinVersion: tls.VersionTLS13,
		}, nil
	case TLSModeMTLS:
		if len(clientCAPEM) == 0 {
			return nil, fmt.Errorf("mtls mode requires a non-empty client CA certificate")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(clientCAPEM) {
			return nil, fmt.Errorf("failed to parse client CA certificate")
		}
		return &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  pool,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported TLS mode: %q", mode)
	}
}
