package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lyafence/chur/internal/provider"
)

func failingFactory(_ context.Context) (provider.SecretProvider, error) {
	return nil, errors.New("temporary error")
}

type mockProvider struct {
	value []byte
	err   error
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return m.value, m.err
}

type retryProvider struct {
	value    []byte
	err      error
	attempts int
	maxFails int
}

func (p *retryProvider) Name() string { return "retry" }
func (p *retryProvider) GetSecret(_ context.Context, _ string) ([]byte, error) {
	p.attempts++
	if p.attempts <= p.maxFails {
		return nil, p.err
	}
	return p.value, nil
}

// initProvider tests

func TestInitProvider_RetriesExhausted(t *testing.T) {
	t.Parallel()
	_, err := initProvider(context.Background(), failingFactory)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestInitProvider_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()
	factory := func(_ context.Context) (provider.SecretProvider, error) {
		return &mockProvider{value: []byte("ok")}, nil
	}
	p, err := initProvider(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secret, err := p.GetSecret(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestInitProvider_SuccessAfterRetries(t *testing.T) {
	t.Parallel()
	attempts := 0
	factory := func(_ context.Context) (provider.SecretProvider, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("not ready yet")
		}
		return &mockProvider{value: []byte("ok")}, nil
	}
	p, err := initProvider(context.Background(), factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secret, err := p.GetSecret(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestInitProvider_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := initProvider(ctx, failingFactory)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestInitProvider_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	slowFactory := func(ctx context.Context) (provider.SecretProvider, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, nil
		}
	}
	_, err := initProvider(ctx, slowFactory)
	if err == nil {
		t.Fatal("expected error due to context timeout")
	}
}

// backoffFetch tests

func TestBackoffFetch_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()
	p := &mockProvider{value: []byte("ok")}
	secret, err := backoffFetch(context.Background(), p, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestBackoffFetch_SuccessAfterRetries(t *testing.T) {
	t.Parallel()
	p := &retryProvider{value: []byte("ok"), err: errors.New("transient error"), maxFails: 2}
	secret, err := backoffFetch(context.Background(), p, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestBackoffFetch_GetSecretFails(t *testing.T) {
	t.Parallel()
	p := &mockProvider{err: errors.New("persistent error")}
	_, err := backoffFetch(context.Background(), p, "test")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestBackoffFetch_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockProvider{err: errors.New("error")}
	_, err := backoffFetch(ctx, p, "test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBackoffFetch_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	slowProvider := &slowProvider{delay: 5 * time.Second}
	_, err := backoffFetch(ctx, slowProvider, "test")
	if err == nil {
		t.Fatal("expected error due to context timeout")
	}
}

type slowProvider struct {
	delay time.Duration
}

func (p *slowProvider) Name() string { return "slow" }
func (p *slowProvider) GetSecret(ctx context.Context, _ string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.delay):
		return []byte("ok"), nil
	}
}
