package keeper

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/lyafence/chur/internal/provider"
)

type KeeperProvider struct {
	url    string
	client *http.Client
}

func NewProvider(url string, skipVerify bool, clientCertFile, clientKeyFile, serverCAFile string) (*KeeperProvider, error) {
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

	return &KeeperProvider{
		url: url + "/v1/secrets/get",
		client: &http.Client{
			Transport: transport,
		},
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
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("keeper: %s: %s", resp.Status, string(respBody))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("keeper: read response: %w", err)
	}
	return data, nil
}

func init() {
	provider.Register("keeper", func(_ context.Context) (provider.SecretProvider, error) {
		url := os.Getenv("CHUR_KEEPER_URL")
		if url == "" {
			url = "https://chur-keeper:9443"
		}
		skipVerify := os.Getenv("CHUR_KEEPER_SKIP_VERIFY") == "1" || os.Getenv("CHUR_KEEPER_SKIP_VERIFY") == "true"
		certFile := os.Getenv("CHUR_KEEPER_TLS_CERT")
		keyFile := os.Getenv("CHUR_KEEPER_TLS_KEY")
		serverCAFile := os.Getenv("CHUR_KEEPER_SERVER_CA")
		return NewProvider(url, skipVerify, certFile, keyFile, serverCAFile)
	})
}
