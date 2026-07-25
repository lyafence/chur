package local

import (
	"context"
	"fmt"
	"os"

	"github.com/lyafence/chur/internal/bytesize"
	"github.com/lyafence/chur/internal/provider"
	"github.com/lyafence/chur/internal/readutil"
	"github.com/lyafence/chur/internal/validate"
)

type LocalProvider struct {
	basePath string
	maxSize  int64
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateSecretRef(ref); err != nil {
		return nil, fmt.Errorf("local: invalid ref: %w", err)
	}

	root, err := os.OpenRoot(p.basePath)
	if err != nil {
		return nil, fmt.Errorf("local: open root %s: %w", p.basePath, err)
	}
	defer root.Close()

	f, err := root.Open(ref)
	if err != nil {
		return nil, fmt.Errorf("local: open %s: %w", ref, err)
	}

	return readutil.ReadWithContext(ctx, f, p.maxSize, "local", ref)
}

func init() {
	provider.Register("local", func(_ context.Context) (provider.SecretProvider, error) {
		basePath := os.Getenv("CHUR_LOCAL_BASE_PATH")
		if basePath == "" {
			basePath = "/etc/chur/secrets"
		}
		if err := validate.ValidateLocalBasePath(basePath); err != nil {
			return nil, fmt.Errorf("local: %w", err)
		}
		maxSize := int64(1 << 20)
		if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
			n, err := bytesize.Parse(v)
			if err != nil {
				return nil, fmt.Errorf("local: invalid CHUR_MAX_SECRET_SIZE %q: %w", v, err)
			}
			if n > 0 {
				maxSize = n
			}
		}
		return &LocalProvider{basePath: basePath, maxSize: maxSize}, nil
	})
}
