package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
)

type LocalProvider struct {
	basePath string
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) GetSecret(_ context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateSecretRef(ref); err != nil {
		return nil, fmt.Errorf("local: invalid ref: %w", err)
	}
	path := filepath.Join(p.basePath, ref)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("local: read %s: %w", path, err)
	}
	return data, nil
}

func init() {
	provider.Register("local", func(_ context.Context) (provider.SecretProvider, error) {
		basePath := "/etc/chur/secrets"
		return &LocalProvider{basePath: basePath}, nil
	})
}
