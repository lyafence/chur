// Package validate provides security-focused validation helpers for chur.
package validate

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateSecretRef ensures ref is a flat, filename-safe string with no
// path separators or traversal sequences. It is used for both the annotation
// value and the resulting file name in the tmpfs mount.
func ValidateSecretRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("secret-ref must not be empty")
	}
	if len(ref) > 255 {
		return fmt.Errorf("secret-ref exceeds 255 characters")
	}
	if ref == "." {
		return fmt.Errorf("secret-ref must not be '.'")
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("secret-ref must not contain '..'")
	}
	if strings.ContainsRune(ref, '/') || strings.ContainsRune(ref, '\\') {
		return fmt.Errorf("secret-ref must not contain path separators")
	}
	for i, r := range ref {
		if !isAllowedRune(r) {
			return fmt.Errorf("secret-ref contains invalid character %q at position %d", r, i)
		}
	}
	first := rune(ref[0])
	if first == '-' || first == '.' {
		return fmt.Errorf("secret-ref must not start with '-' or '.'")
	}
	last := rune(ref[len(ref)-1])
	if last == '-' || last == '.' {
		return fmt.Errorf("secret-ref must not end with '-' or '.'")
	}
	return nil
}

// isAllowedRune restricts secret refs, keys, and mount paths to ASCII
// alphanumerics, hyphens, underscores, and dots. Unicode letters are rejected
// to prevent homoglyph attacks (e.g. Cyrillic 'а' vs ASCII 'a'). This is a
// security hardening decision, not a platform limitation.
func isAllowedRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
}

// ValidateSecretKey validates a Kubernetes Secret data key name. Kubernetes
// allows keys to consist of alphanumeric characters, '-', '_', or '.'.
func ValidateSecretKey(key string) error {
	if key == "" {
		return nil // key is optional
	}
	if len(key) > 253 {
		return fmt.Errorf("secret-key exceeds 253 characters")
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("secret-key must not contain '..'")
	}
	if strings.ContainsRune(key, '/') || strings.ContainsRune(key, '\\') {
		return fmt.Errorf("secret-key must not contain path separators")
	}
	for i, r := range key {
		if !isAllowedRune(r) {
			return fmt.Errorf("secret-key contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}

// ValidateMountPath ensures path is a safe absolute path with no traversal
// sequences or special characters. Empty is allowed (caller defaults to /secrets).
func ValidateMountPath(path string) error {
	if path == "" {
		return nil
	}
	if len(path) > 4096 {
		return fmt.Errorf("mount-path exceeds 4096 characters")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("mount-path must be absolute")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("mount-path must not contain '..'")
	}
	for i, r := range path {
		if !isAllowedMountRune(r) {
			return fmt.Errorf("mount-path contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}

func isAllowedMountRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '/' || r == '-' || r == '_' || r == '.'
}

// ValidateLocalBasePath validates the CHUR_LOCAL_BASE_PATH configuration value.
// It must be a non-root absolute path with restricted characters.
func ValidateLocalBasePath(path string) error {
	if path == "" {
		return fmt.Errorf("local base path must not be empty")
	}
	if len(path) > 4096 {
		return fmt.Errorf("local base path exceeds 4096 characters")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("local base path must be absolute")
	}
	cleaned := filepath.Clean(path)
	if cleaned == "/" || cleaned == "." {
		return fmt.Errorf("local base path must not be root")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("local base path must not contain '..'")
	}
	for i, r := range path {
		if !isAllowedMountRune(r) {
			return fmt.Errorf("local base path contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}

// ValidateKeeperRef validates a ref used by the keeper provider.
// It allows hierarchical names with '/' but forbids traversal and absolute paths.
func ValidateKeeperRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("keeper ref must not be empty")
	}
	if len(ref) > 4096 {
		return fmt.Errorf("keeper ref exceeds 4096 characters")
	}
	if ref[0] == '/' {
		return fmt.Errorf("keeper ref must not be an absolute path")
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("keeper ref must not contain '..'")
	}

	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-"
	for i, r := range ref {
		if !strings.ContainsRune(allowed, r) {
			return fmt.Errorf("keeper ref contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}
