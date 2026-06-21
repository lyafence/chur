package env

import (
	"context"
	"fmt"
	"os"

	"github.com/lyafence/chur/internal/provider"
)

type EnvProvider struct{}

func (p *EnvProvider) Name() string { return "env" }

func (p *EnvProvider) GetSecret(_ context.Context, ref string) ([]byte, error) {
	value, ok := os.LookupEnv(ref)
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
