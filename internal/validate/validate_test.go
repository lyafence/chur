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
		{"cyrillic", "secret-а", true},
		{"chinese", "秘密", true},
		{"arabic", "سر", true},
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

func TestValidateSecretRef_ErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     string
		wantMsg string
	}{
		{"empty", "", "must not be empty"},
		{"too-long", strings.Repeat("a", 256), "exceeds 255 characters"},
		{"dot", ".", "must not be '.'"},
		{"dotdot", "..", "must not contain '..'"},
		{"slash", "foo/bar", "must not contain path separators"},
		{"backslash", "foo\\bar", "must not contain path separators"},
		{"special", "secret!", "contains invalid character"},
		{"cyrillic", "secret-а", "contains invalid character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretRef(tt.ref)
			if err == nil {
				t.Fatalf("expected error for %q", tt.ref)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("ValidateSecretRef(%q) error = %v, want contains %q", tt.ref, err, tt.wantMsg)
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
		{"cyrillic", "/секреты", true},
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

func TestValidateMountPath_ErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantMsg string
	}{
		{"relative", "relative/path", "must be absolute"},
		{"dotdot", "/etc/../secrets", "must not contain '..'"},
		{"special", "/path/with!special", "contains invalid character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.path)
			if err == nil {
				t.Fatalf("expected error for %q", tt.path)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("ValidateMountPath(%q) error = %v, want contains %q", tt.path, err, tt.wantMsg)
			}
		})
	}
}

func TestValidateKeeperRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ref   string
		valid bool
	}{
		{"simple path", "prod/db/password", true},
		{"simple name", "simple", true},
		{"empty", "", false},
		{"absolute", "/absolute", false},
		{"traversal", "../etc/passwd", false},
		{"backslash", "a\\b", false},
		{"null byte", "a\x00b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKeeperRef(tc.ref)
			if tc.valid && err != nil {
				t.Errorf("ValidateKeeperRef(%q) unexpected error: %v", tc.ref, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("ValidateKeeperRef(%q) expected error", tc.ref)
			}
		})
	}
}

func TestValidateKeeperRef_ErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     string
		wantMsg string
	}{
		{"empty", "", "must not be empty"},
		{"absolute", "/absolute", "must not be an absolute path"},
		{"traversal", "../etc/passwd", "must not contain '..'"},
		{"backslash", "a\\b", "contains invalid character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKeeperRef(tt.ref)
			if err == nil {
				t.Fatalf("expected error for %q", tt.ref)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("ValidateKeeperRef(%q) error = %v, want contains %q", tt.ref, err, tt.wantMsg)
			}
		})
	}
}

func TestValidateLocalBasePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"simple", "/etc/chur/secrets", false},
		{"nested", "/var/lib/chur/secrets", false},
		{"empty", "", true},
		{"root", "/", true},
		{"double-slash", "//", true},
		{"dot-slash", "/.", true},
		{"relative", "etc/chur", true},
		{"dotdot", "/etc/../secrets", true},
		{"space", "/path/with space", true},
		{"special", "/path/with!chars", true},
		{"too-long", "/" + strings.Repeat("a", 4096), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalBasePath(tt.path)
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
