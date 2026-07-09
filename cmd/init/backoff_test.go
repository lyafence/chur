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

func TestBackoffFetch_RetriesExhausted(t *testing.T) {
	t.Parallel()
	_, err := backoffFetch(context.Background(), failingFactory, "test")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestBackoffFetch_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()
	factory := func(_ context.Context) (provider.SecretProvider, error) {
		return &mockProvider{value: []byte("ok")}, nil
	}
	secret, err := backoffFetch(context.Background(), factory, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestBackoffFetch_SuccessAfterRetries(t *testing.T) {
	t.Parallel()
	attempts := 0
	factory := func(_ context.Context) (provider.SecretProvider, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("not ready yet")
		}
		return &mockProvider{value: []byte("ok")}, nil
	}
	secret, err := backoffFetch(context.Background(), factory, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "ok" {
		t.Fatalf("expected %q, got %q", "ok", string(secret))
	}
}

func TestBackoffFetch_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := backoffFetch(ctx, failingFactory, "test")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBackoffFetch_ContextTimeout(t *testing.T) {
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
	_, err := backoffFetch(ctx, slowFactory, "test")
	if err == nil {
		t.Fatal("expected error due to context timeout")
	}
}
