// Package readutil provides context-aware bounded reads.
package readutil

import (
	"context"
	"fmt"
	"io"
)

// ReadWithContext reads from r with a context-aware cancelation goroutine.
// maxSize limits the bytes read (reads maxSize+1 to detect overflow).
// errLabel is a prefix for error messages (e.g. "local", "filesystem").
// ref is included in read error messages for debuggability.
func ReadWithContext(ctx context.Context, r io.ReadCloser, maxSize int64, errLabel, ref string) ([]byte, error) {
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
		ch <- readResult{data, err}
	}()

	select {
	case <-ctx.Done():
		r.Close()
		<-ch
		return nil, fmt.Errorf("%s: %w", errLabel, ctx.Err())
	case res := <-ch:
		r.Close()
		if res.err != nil {
			return nil, fmt.Errorf("%s: read %q: %w", errLabel, ref, res.err)
		}
		if int64(len(res.data)) > maxSize {
			return nil, fmt.Errorf("%s: secret exceeds max size", errLabel)
		}
		return res.data, nil
	}
}
