package readutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadWithContext_Success(t *testing.T) {
	t.Parallel()
	r := io.NopCloser(strings.NewReader("hello"))
	data, err := ReadWithContext(context.Background(), r, 1024, "test", "my-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", string(data), "hello")
	}
}

func TestReadWithContext_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := io.NopCloser(strings.NewReader("data"))
	_, err := ReadWithContext(ctx, r, 1024, "test", "ref")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestReadWithContext_Oversize(t *testing.T) {
	t.Parallel()
	r := io.NopCloser(strings.NewReader("hello world"))
	_, err := ReadWithContext(context.Background(), r, 5, "test", "ref")
	if err == nil {
		t.Fatal("expected error for oversized data")
	}
}

func TestReadWithContext_ReadError(t *testing.T) {
	t.Parallel()
	r := io.NopCloser(&errReader{err: io.ErrUnexpectedEOF})
	_, err := ReadWithContext(context.Background(), r, 1024, "test", "ref")
	if err == nil {
		t.Fatal("expected read error")
	}
}

type errReader struct {
	err error
}

func (r *errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestReadWithContext_EmptyInput(t *testing.T) {
	t.Parallel()
	r := io.NopCloser(strings.NewReader(""))
	data, err := ReadWithContext(context.Background(), r, 1024, "test", "ref")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
}

func TestReadWithContext_ExactSize(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("A"), 1024)
	r := io.NopCloser(bytes.NewReader(payload))
	data, err := ReadWithContext(context.Background(), r, 1024, "test", "ref")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 1024 {
		t.Errorf("expected 1024 bytes, got %d", len(data))
	}
}
