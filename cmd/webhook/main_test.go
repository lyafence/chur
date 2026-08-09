package main

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
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

func TestParseMaxSecretSizeEnv(t *testing.T) {
	t.Parallel()
	limit := resource.MustParse("10Mi")
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty ok", "", false},
		{"binary suffix ok", "1Mi", false},
		{"plain bytes ok", "1048576", false},
		{"decimal SI rejected (init cannot parse)", "1M", true},
		{"fractional binary rejected", "1.5Mi", true},
		{"milli rejected", "100m", true},
		{"over volume limit rejected", "11Mi", true},
		{"garbage rejected", "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMaxSecretSizeEnv(tt.value, limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseMaxSecretSizeEnv(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestParseMaxSecretSizeEnv_SmallVolumeLimit(t *testing.T) {
	t.Parallel()
	_, err := parseMaxSecretSizeEnv("1Mi", resource.MustParse("512Ki"))
	if err == nil {
		t.Fatal("expected error: default max secret size exceeds volume limit")
	}
}
