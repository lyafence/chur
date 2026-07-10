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

type ExecBackend struct {
	command   string
	timeout   time.Duration
	maxStdout int64
}

func New(command string, timeout time.Duration, maxStdout int64) *ExecBackend {
	return &ExecBackend{command: command, timeout: timeout, maxStdout: maxStdout}
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec: start: %w", err)
	}

	var out bytes.Buffer
	if b.maxStdout > 0 {
		n, err := io.CopyN(&out, stdout, b.maxStdout+1)
		if n > b.maxStdout {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("exec: stdout exceeds max size (%d bytes)", b.maxStdout)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("exec: read stdout: %w", err)
		}
	} else {
		if _, err := out.ReadFrom(stdout); err != nil {
			return nil, fmt.Errorf("exec: read stdout: %w", err)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("exec: command timed out")
		}
		return nil, fmt.Errorf("exec: %s: %w (stderr: %s)", b.command, err, stderr.String())
	}

	return out.Bytes(), nil
}
