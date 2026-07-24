package main

import (
	"fmt"
	"testing"
)

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b, want string
	}{
		{"hello", "world", "hello"},
		{"", "world", "world"},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q_%q", tt.a, tt.b), func(t *testing.T) {
			if got := firstNonEmpty(tt.a, tt.b); got != tt.want {
				t.Errorf("firstNonEmpty(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestValidateDNS1123Label(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		label string
		valid bool
	}{
		{"simple", "default", true},
		{"hyphen", "kube-system", true},
		{"alphanumeric", "my-namespace-42", true},
		{"empty", "", false},
		{"leading dash", "-leading", false},
		{"trailing dash", "trailing-", false},
		{"uppercase", "UPPERCASE", false},
		{"space", "has space", false},
		{"single char", "a", true},
		{"numeric", "123", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDNS1123Label(tt.label)
			if tt.valid && err != nil {
				t.Errorf("validateDNS1123Label(%q) = %v, want nil", tt.label, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("validateDNS1123Label(%q) = nil, want error", tt.label)
			}
		})
	}
}
