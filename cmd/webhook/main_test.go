package main

import (
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
		if got := firstNonEmpty(tt.a, tt.b); got != tt.want {
			t.Errorf("firstNonEmpty(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidateDNS1123Label(t *testing.T) {
	t.Parallel()
	tests := []struct {
		label string
		valid bool
	}{
		{"default", true},
		{"kube-system", true},
		{"my-namespace-42", true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"UPPERCASE", false},
		{"has space", false},
		{"a", true},
		{"123", true},
	}
	for _, tt := range tests {
		err := validateDNS1123Label(tt.label)
		if tt.valid && err != nil {
			t.Errorf("validateDNS1123Label(%q) = %v, want nil", tt.label, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateDNS1123Label(%q) = nil, want error", tt.label)
		}
	}
}
