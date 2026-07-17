package bytesize

import (
	"fmt"
	"math"
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
	case strings.HasSuffix(s, "Gi") || strings.HasSuffix(s, "gi"):
		multiplier = 1 << 30
		num = s[:len(s)-2]
	case strings.HasSuffix(s, "Mi") || strings.HasSuffix(s, "mi"):
		multiplier = 1 << 20
		num = s[:len(s)-2]
	case strings.HasSuffix(s, "Ki") || strings.HasSuffix(s, "ki"):
		multiplier = 1 << 10
		num = s[:len(s)-2]
	}

	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative size %d", n)
	}
	if n > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("size overflow %d", n)
	}
	return n * multiplier, nil
}
