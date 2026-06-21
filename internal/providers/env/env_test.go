package env

import (
	"context"
	"testing"
)

func TestEnvProvider_GetSecret(t *testing.T) {
	t.Setenv("CHUR_TEST_SECRET", "hunter2")

	p := &EnvProvider{}
	ctx := context.Background()

	secret, err := p.GetSecret(ctx, "CHUR_TEST_SECRET")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(secret) != "hunter2" {
		t.Fatalf("expected %q, got %q", "hunter2", string(secret))
	}

	_, err = p.GetSecret(ctx, "CHUR_MISSING_SECRET")
	if err == nil {
		t.Fatal("expected error for missing variable")
	}
}
