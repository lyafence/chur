package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// parseSize converts a size string (plain int, Ki, Mi, Gi) to bytes.
// Returns 1MiB default for empty or invalid input.
func parseSize(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 1 << 20
	}
	var mult int64 = 1
	switch {
	case strings.HasSuffix(v, "Gi"):
		mult = 1 << 30
		v = strings.TrimSuffix(v, "Gi")
	case strings.HasSuffix(v, "Mi"):
		mult = 1 << 20
		v = strings.TrimSuffix(v, "Mi")
	case strings.HasSuffix(v, "Ki"):
		mult = 1 << 10
		v = strings.TrimSuffix(v, "Ki")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 1 << 20
	}
	return n * mult
}

func init() {
	provider.Register("local", func(_ context.Context) (provider.SecretProvider, error) {
		basePath := os.Getenv("CHUR_LOCAL_BASE_PATH")
		if basePath == "" {
			basePath = "/etc/chur/secrets"
		}
		maxSize := int64(1 << 20)
		if v := os.Getenv("CHUR_MAX_SECRET_SIZE"); v != "" {
			maxSize = parseSize(v)
		}
		return &LocalProvider{basePath: basePath, maxSize: maxSize}, nil
	})
}
