package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lyafence/chur/internal/validate"
)

type HTTPBackend struct {
	baseURL string
	token   string
	maxSize int64
	client  *http.Client
}

func (b *HTTPBackend) Name() string { return "http" }

func New(baseURL, tokenFile string, timeout time.Duration, maxSize int64) (*HTTPBackend, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("http: baseURL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("http: baseURL must be an absolute https:// URL")
	}

	var token string
	if tokenFile != "" {
		b, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("http: read token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
		token = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, token)
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("http: maxSize must be positive, got %d", maxSize)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	return &HTTPBackend{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		maxSize: maxSize,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

func (b *HTTPBackend) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("http: invalid ref: %w", err)
	}

	endpoint := b.baseURL + "?ref=" + url.QueryEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("http: create request: %w", err)
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http: %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, b.maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("http: read: %w", err)
	}
	if int64(len(data)) > b.maxSize {
		return nil, fmt.Errorf("http: secret exceeds max size")
	}
	return data, nil
}
