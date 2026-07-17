package keeper

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lyafence/chur/internal/bytesize"
	"github.com/lyafence/chur/internal/provider"
)

type KeeperProvider struct {
	url           string
	client        *http.Client
	maxSecretSize int64
}

func NewProvider(rawURL string, skipVerify bool, clientCertFile, clientKeyFile, serverCAFile string) (*KeeperProvider, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	if skipVerify {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	if serverCAFile != "" {
		caPEM, err := os.ReadFile(serverCAFile)
		if err != nil {
			return nil, fmt.Errorf("keeper: read server CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("keeper: failed to parse server CA")
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	if clientCertFile != "" && clientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("keeper: load client cert: %w", err)
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}

	endpoint, err := url.JoinPath(rawURL, "/v1/secrets/get")
	if err != nil {
		return nil, fmt.Errorf("keeper: invalid URL: %w", err)
	}

	return &KeeperProvider{
		url: endpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		maxSecretSize: 1 << 20,
	}, nil
}

func (p *KeeperProvider) Name() string { return "keeper" }

func (p *KeeperProvider) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"ref": ref})
	if err != nil {
		return nil, fmt.Errorf("keeper: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("keeper: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("keeper: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			slog.WarnContext(ctx, "keeper: failed to read error response body", "status", resp.Status, "error", err)
		}
		return nil, fmt.Errorf("keeper: %s: %s", resp.Status, string(respBody))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, p.maxSecretSize+1))
	if err != nil {
		return nil, fmt.Errorf("keeper: read response: %w", err)
	}
	if int64(len(data)) > p.maxSecretSize {
		return nil, fmt.Errorf("keeper: response exceeds max size")
	}
	return data, nil
}

func init() {
	provider.Register("keeper", func(_ context.Context) (provider.SecretProvider, error) {
		url := os.Getenv("CHUR_KEEPER_URL")
		if url == "" {
			url = "https://chur-keeper.chur-system.svc:9443"
		}
		skipVerify := os.Getenv("CHUR_KEEPER_SKIP_VERIFY") == "1" || os.Getenv("CHUR_KEEPER_SKIP_VERIFY") == "true"
		certFile := os.Getenv("CHUR_KEEPER_TLS_CERT_PATH")
		keyFile := os.Getenv("CHUR_KEEPER_TLS_KEY_PATH")
		serverCAFile := os.Getenv("CHUR_KEEPER_SERVER_CA")
		p, err := NewProvider(url, skipVerify, certFile, keyFile, serverCAFile)
		if err != nil {
			return nil, err
		}
		if v := os.Getenv("CHUR_KEEPER_CLIENT_MAX_SECRET_SIZE"); v != "" {
			n, err := bytesize.Parse(v)
			if err != nil {
				return nil, fmt.Errorf("invalid CHUR_KEEPER_CLIENT_MAX_SECRET_SIZE %q: %w", v, err)
			}
			if n > 0 {
				p.maxSecretSize = n
			}
		}
		return p, nil
	})
}
