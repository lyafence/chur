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

func (b *FSBackend) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("filesystem: invalid ref: %w", err)
	}

	f, err := b.root.Open(ref)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open %q: %w", ref, err)
	}
	defer f.Close()

	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(f, b.maxSize+1))
		ch <- readResult{data, err}
	}()

	select {
	case <-ctx.Done():
		f.Close()
		<-ch
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("filesystem: read %q: %w", ref, r.err)
		}
		if int64(len(r.data)) > b.maxSize {
			return nil, fmt.Errorf("filesystem: secret exceeds max size")
		}
		return r.data, nil
	}
}

func NewWithMaxSize(root string, maxSize int64) (*FSBackend, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open root %q: %w", root, err)
	}
	return &FSBackend{root: r, maxSize: maxSize}, nil
}
