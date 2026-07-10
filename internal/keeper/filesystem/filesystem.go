package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lyafence/chur/internal/validate"
)

type FSBackend struct {
	Root string
}

func (b *FSBackend) Name() string { return "filesystem" }

func (b *FSBackend) GetSecret(_ context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("filesystem: invalid ref: %w", err)
	}

	path := filepath.Join(b.Root, ref)
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("filesystem: clean path: %w", err)
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(b.Root))
	if err != nil {
		return nil, fmt.Errorf("filesystem: clean root: %w", err)
	}

	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
		return nil, fmt.Errorf("filesystem: path traversal denied: %q", ref)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("filesystem: read %s: %w", cleanPath, err)
	}
	return data, nil
}

func New(root string) *FSBackend {
	return &FSBackend{Root: root}
}
