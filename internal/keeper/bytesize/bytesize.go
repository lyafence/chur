package bytesize

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse converts a size string to bytes. Supports plain integers and Ki/Mi/Gi suffixes.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	var multiplier int64 = 1
	num := s
	switch {
	case strings.HasSuffix(s, "Gi"):
		multiplier = 1 << 30
		num = strings.TrimSuffix(s, "Gi")
	case strings.HasSuffix(s, "Mi"):
		multiplier = 1 << 20
		num = strings.TrimSuffix(s, "Mi")
	case strings.HasSuffix(s, "Ki"):
		multiplier = 1 << 10
		num = strings.TrimSuffix(s, "Ki")
	}

	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative size %d", n)
	}
	return n * multiplier, nil
}
