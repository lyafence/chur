package logsafe

import "testing"

func TestValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"\n", "?"},
		{"\r", "?"},
		{"\r\n", "??"},
		{"\x1b[31m", "?[31m"},
		{"\t", "?"},
		{"prod/db", "prod/db"},
		{"a\x00b", "a?b"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Value(tt.in); got != tt.want {
				t.Errorf("Value(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
