package filesystem

import (
	"context"
	"fmt"
	"os"

	"github.com/lyafence/chur/internal/readutil"
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

func (b *FSBackend) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("filesystem: invalid ref: %w", err)
	}

	f, err := b.root.Open(ref)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open %q: %w", ref, err)
	}

	return readutil.ReadWithContext(ctx, f, b.maxSize, "filesystem", ref)
}

func NewWithMaxSize(root string, maxSize int64) (*FSBackend, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open root %q: %w", root, err)
	}
	return &FSBackend{root: r, maxSize: maxSize}, nil
}
