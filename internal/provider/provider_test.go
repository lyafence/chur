package provider

import (
	"context"
	"strings"
	"testing"
)

func TestGetReturnsFalseForUnknown(t *testing.T) {
	t.Parallel()
	_, ok := Get("unknown-provider")
	if ok {
		t.Error("expected Get for unknown provider to return false")
	}
}

func TestIsValidNameTrueForRegistered(t *testing.T) {
	t.Parallel()
	// All providers that are registered should be recognized.
	// Which providers are available depends on build tags at test time.
	for name := range registry {
		if !IsValidName(name) {
			t.Errorf("IsValidName(%q) = false after Register()", name)
		}
	}
}

func TestFactoriesCreate(t *testing.T) {
	t.Parallel()
	// Smoke-test that registered factories return a non-nil provider.
	// The k8s provider may fail outside a cluster — skip that case.
	for name, factory := range registry {
		p, err := factory(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "in-cluster config") {
				continue
			}
			t.Errorf("factory for %q returned error: %v", name, err)
		}
		if p == nil {
			t.Errorf("factory for %q returned nil", name)
		}
	}
}
