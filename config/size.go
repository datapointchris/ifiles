package config

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	factor int64
}{
	// Longest suffixes first: "MiB" has to match before "B" would.
	{"KIB", 1 << 10},
	{"MIB", 1 << 20},
	{"GIB", 1 << 30},
	{"KB", 1000},
	{"MB", 1000 * 1000},
	{"GB", 1000 * 1000 * 1000},
	{"K", 1 << 10},
	{"M", 1 << 20},
	{"G", 1 << 30},
	{"B", 1},
}

// ParseSize converts a human-written byte size ("32MiB", "500KB", "1048576")
// into bytes. Binary and decimal suffixes are distinguished, because the value
// is compared against a hard 100 MB transfer limit where a 4.8% difference is
// the margin between working and a 413.
func ParseSize(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty size")
	}

	upper := strings.ToUpper(trimmed)
	for _, unit := range sizeUnits {
		if !strings.HasSuffix(upper, unit.suffix) {
			continue
		}
		digits := strings.TrimSpace(upper[:len(upper)-len(unit.suffix)])
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q", s)
		}
		if value <= 0 {
			return 0, fmt.Errorf("size %q must be positive", s)
		}
		if value > (1<<62)/unit.factor {
			return 0, fmt.Errorf("size %q overflows", s)
		}
		return value * unit.factor, nil
	}

	value, err := strconv.ParseInt(upper, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: want bytes or a suffix such as 32MiB", s)
	}
	if value <= 0 {
		return 0, fmt.Errorf("size %q must be positive", s)
	}
	return value, nil
}

// FormatSize renders a byte count for display, using binary units.
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
