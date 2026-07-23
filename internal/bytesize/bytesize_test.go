package bytesize

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int64
	}{
		{"1048576", 1048576},
		{"1Mi", 1048576},
		{"512Ki", 524288},
		{"2Gi", 2 << 30},
		{"0", 0},
		{"1ki", 1024},
		{" 1Mi ", 1048576},
	}

	for _, tc := range tests {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "-1", "1Xi", "1.5Mi", "1KI", "1MiB", "9223372036854775807Ki"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) expected error", in)
		}
	}
}
