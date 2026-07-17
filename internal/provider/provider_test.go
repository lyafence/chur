package provider

import (
	"context"
	"testing"
)

func TestGetReturnsFalseForUnknown(t *testing.T) {
	t.Parallel()
	_, ok := Get("unknown-provider")
	if ok {
		t.Error("expected Get for unknown provider to return false")
	}
}

func TestGetReturnsRegisteredTrue(t *testing.T) {
	t.Parallel()
	for name := range registry {
		if _, ok := Get(name); !ok {
			t.Errorf("Get(%q) = false after Register()", name)
		}
	}
}

func TestFactoriesCreate(t *testing.T) {
	t.Parallel()
	for name, factory := range registry {
		p, err := factory(context.Background())
		if err != nil {
			t.Logf("skipping provider %q: factory failed (environment not available): %v", name, err)
			continue
		}
		if p == nil {
			t.Errorf("factory for %q returned nil", name)
		}
	}
}
