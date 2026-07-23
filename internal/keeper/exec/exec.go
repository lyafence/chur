package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/lyafence/chur/internal/validate"
)

type limitedWriter struct {
	buf      bytes.Buffer
	limit    int64
	overflow bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - int64(w.buf.Len())
	if remaining <= 0 {
		if !w.overflow {
			w.overflow = true
			w.buf.WriteString("...(truncated)")
		}
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		w.buf.Write(p[:remaining])
		if !w.overflow {
			w.overflow = true
			w.buf.WriteString("...(truncated)")
		}
		return len(p), nil
	}
	return w.buf.Write(p)
}

func (w *limitedWriter) String() string {
	return w.buf.String()
}

type ExecBackend struct {
	command   string
	timeout   time.Duration
	maxStdout int64
}

func New(command string, timeout time.Duration, maxStdout int64) (*ExecBackend, error) {
	if maxStdout <= 0 {
		return nil, fmt.Errorf("exec: maxStdout must be positive, got %d", maxStdout)
	}
	return &ExecBackend{command: command, timeout: timeout, maxStdout: maxStdout}, nil
}

func (b *ExecBackend) Name() string { return "exec" }

func (b *ExecBackend) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	if err := validate.ValidateKeeperRef(ref); err != nil {
		return nil, fmt.Errorf("exec: invalid ref: %w", err)
	}

	timeout := b.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.command, ref)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("exec: stdout pipe: %w", err)
	}
	stderrLimit := b.maxStdout
	if stderrLimit <= 0 {
		stderrLimit = 1 << 20 // 1 MiB default
	}
	stderr := &limitedWriter{limit: stderrLimit}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		stdout.Close()
		return nil, fmt.Errorf("exec: start: %w", err)
	}

	var out bytes.Buffer
	n, copyErr := io.CopyN(&out, stdout, b.maxStdout+1)
	stdout.Close()

	if n > b.maxStdout {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("exec: stdout exceeds max size (%d bytes)", b.maxStdout)
	}

	waitErr := cmd.Wait()
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return nil, fmt.Errorf("exec: read stdout: %w", copyErr)
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("exec: command timed out")
		}
		stderrMsg := stderr.String()
		return nil, fmt.Errorf("exec: %s: %w (stderr: %s)", b.command, waitErr, stderrMsg)
	}

	return out.Bytes(), nil
}
