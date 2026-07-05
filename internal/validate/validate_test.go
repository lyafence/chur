package validate

import (
	"strings"
	"testing"
)

func TestValidateSecretRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"simple", "my-secret", false},
		{"with-dots", "my.secret.ref", false},
		{"with-underscore", "my_secret", false},
		{"mixed", "secret-v1.2.3", false},
		{"empty", "", true},
		{"too-long", strings.Repeat("a", 256), true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"contains-dotdot", "foo..bar", true},
		{"slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
		{"start-dash", "-secret", true},
		{"end-dash", "secret-", true},
		{"start-dot", ".secret", true},
		{"end-dot", "secret.", true},
		{"space", "secret name", true},
		{"special", "secret!", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretRef(tt.ref)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.ref)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.ref, err)
			}
		})
	}
}

func TestValidateMountPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty", "", false},
		{"root", "/", false},
		{"simple", "/secrets", false},
		{"nested", "/etc/secrets", false},
		{"with-dash", "/path/with-dash", false},
		{"with-dot", "/path/with.dot", false},
		{"long-path", "/" + strings.Repeat("a/b/", 100) + "end", false},
		{"relative", "relative/path", true},
		{"dotdot", "/etc/../secrets", true},
		{"traversal", "/../../etc", true},
		{"space", "/path/with space", true},
		{"semicolon", "/path/with;chars", true},
		{"special", "/path/with!special", true},
		{"too-long", "/" + strings.Repeat("a", 4096), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.path)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.path, err)
			}
		})
	}
}

func TestValidateSecretKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"empty", "", false},
		{"simple", "token", false},
		{"with-dots", "tls.crt", false},
		{"start-dot", ".gitconfig", false},
		{"too-long", strings.Repeat("a", 254), true},
		{"slash", "foo/bar", true},
		{"dotdot", "..", true},
		{"space", "secret key", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretKey(tt.key)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.key)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.key, err)
			}
		})
	}
}
