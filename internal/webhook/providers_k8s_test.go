//go:build provider_k8s

package webhook

import (
	"testing"

	"github.com/lyafence/chur/internal/provider"
	_ "github.com/lyafence/chur/internal/providers/k8s"
)

// TestValidProvidersAllRegistered asserts every provider the webhook can admit
// exists in the compiled registry. It lives in a provider_k8s-tagged file
// because the minimal build (without the tag) intentionally omits the k8s
// provider — admission-time validation does not guarantee runtime
// availability there (see validProviders doc in mutator.go).
func TestValidProvidersAllRegistered(t *testing.T) {
	t.Parallel()
	registered := make(map[string]bool)
	for _, name := range provider.Names() {
		registered[name] = true
	}
	for name := range validProviders {
		if !registered[name] {
			t.Errorf("webhook admits provider %q but registry has no such provider", name)
		}
	}
}
