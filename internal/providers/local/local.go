package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lyafence/chur/internal/bytesize"
	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/validate"
)

type LocalProvider struct {
	basePath string
	maxSize  int64
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) GetSecret(_ context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateSecretRef(ref); err != nil {
		return nil, fmt.Errorf("local: invalid ref: %w", err)
	}
	path := filepath.Join(p.basePath, ref)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("local: open %s: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, p.maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("local: read %s: %w", path, err)
	}
	if int64(len(data)) > p.maxSize {
		return nil, fmt.Errorf("local: secret exceeds max size")
	}
	return data, nil
}

func init() {
	provider.Register("local", func(_ context.Context) (provider.SecretProvider, error) {
		basePath := os.Getenv("CHUR_LOCAL_BASE_PATH")
		if basePath == "" {
			basePath = "/etc/chur/secrets"
		}
		maxSize := int64(1 << 20)
		if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
			n, err := bytesize.Parse(v)
			if err != nil {
				return nil, fmt.Errorf("invalid CHUR_MAX_SECRET_SIZE %q: %w", v, err)
			}
			if n > 0 {
				maxSize = n
			}
		}
		return &LocalProvider{basePath: basePath, maxSize: maxSize}, nil
	})
}
