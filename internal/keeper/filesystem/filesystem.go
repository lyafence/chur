package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/lyafence/chur/internal/validate"
)

type FSBackend struct {
	root    *os.Root
	maxSize int64
}

func (b *FSBackend) Name() string { return "filesystem" }

func (b *FSBackend) Close() error {
	return b.root.Close()
}

func (b *FSBackend) GetSecret(_ context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("filesystem: invalid ref: %w", err)
	}

	f, err := b.root.Open(ref)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open %q: %w", ref, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, b.maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("filesystem: read %q: %w", ref, err)
	}
	if int64(len(data)) > b.maxSize {
		return nil, fmt.Errorf("filesystem: secret exceeds max size")
	}
	return data, nil
}

func NewWithMaxSize(root string, maxSize int64) (*FSBackend, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open root %q: %w", root, err)
	}
	return &FSBackend{root: r, maxSize: maxSize}, nil
}
