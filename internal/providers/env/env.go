package env

import (
	"context"
	"fmt"
	"os"

	"github.com/lyafence/chur/internal/provider"
)

type EnvProvider struct {
	lookupEnv func(string) (string, bool)
}

func (p *EnvProvider) Name() string { return "env" }

func (p *EnvProvider) GetSecret(_ context.Context, ref string) ([]byte, error) {
	lookup := p.lookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(ref)
	if !ok {
		return nil, fmt.Errorf("env: variable %q is not set", ref)
	}
	return []byte(value), nil
}

func init() {
	provider.Register("env", func(_ context.Context) (provider.SecretProvider, error) {
		return &EnvProvider{}, nil
	})
}
