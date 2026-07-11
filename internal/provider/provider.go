package provider

import (
	"context"
	"sort"
	"sync"
)

// SecretProvider defines the interface for fetching secrets from any backend.
type SecretProvider interface {
	// Name returns the provider name (e.g. "env", "local", "k8s", "keeper").
	Name() string
	// GetSecret fetches a secret by reference (name, path, ARN, etc.).
	GetSecret(ctx context.Context, ref string) ([]byte, error)
}

// Factory creates a SecretProvider.
type Factory func(ctx context.Context) (SecretProvider, error)

var (
	registry   = map[string]Factory{}
	registryMu sync.RWMutex
)

// Register adds a provider factory to the registry. It is safe for concurrent use.
func Register(name string, f Factory) {
	if name == "" {
		panic("provider.Register: name must not be empty")
	}
	if f == nil {
		panic("provider.Register: factory must not be nil")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("provider.Register: duplicate provider name: " + name)
	}
	registry[name] = f
}

// Get retrieves a registered factory by name. It is safe for concurrent use.
func Get(name string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	return f, ok
}

// Names returns all registered provider names, sorted. It is safe for concurrent use.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
