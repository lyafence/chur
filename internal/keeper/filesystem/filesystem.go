package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lyafence/chur/internal/validate"
)

type FSBackend struct {
	root    string
	maxSize int64
}

func (b *FSBackend) Name() string { return "filesystem" }

func (b *FSBackend) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("filesystem: invalid ref: %w", err)
	}

	path := filepath.Join(b.root, ref)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("filesystem: stat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("filesystem: symlink denied: %q", ref)
	}

	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("filesystem: clean path: %w", err)
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(b.root))
	if err != nil {
		return nil, fmt.Errorf("filesystem: clean root: %w", err)
	}

	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
		return nil, fmt.Errorf("filesystem: path traversal denied: %q", ref)
	}

	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("filesystem: open %s: %w", cleanPath, err)
	}
	defer f.Close()

	// Post-open stat: verify the opened file matches the pre-open Lstat.
	// This prevents TOCTOU — an attacker replacing the file with a symlink
	// between the Lstat check above and Open here.
	fi2, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("filesystem: stat opened %s: %w", cleanPath, err)
	}
	if !os.SameFile(fi, fi2) {
		return nil, fmt.Errorf("filesystem: file replaced with symlink: %q", ref)
	}

	// Read with limit in a goroutine so we respect ctx cancellation.
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
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("filesystem: read %s: %w", cleanPath, r.err)
		}
		if int64(len(r.data)) > b.maxSize {
			return nil, fmt.Errorf("filesystem: secret exceeds max size")
		}
		return r.data, nil
	}
}

func New(root string) *FSBackend {
	return &FSBackend{root: root}
}

func NewWithMaxSize(root string, maxSize int64) *FSBackend {
	return &FSBackend{root: root, maxSize: maxSize}
}
