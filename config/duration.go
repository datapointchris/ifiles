package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var durationUnits = []struct {
	suffix string
	factor time.Duration
}{
	{"W", 7 * 24 * time.Hour},
	{"D", 24 * time.Hour},
}

// ParseDuration converts a human-written validity window ("7d", "12h", "90m")
// into a Duration.
//
// time.ParseDuration stops at hours, and days are the unit a share link or an
// API token is actually thought about in, so d and w are converted here and
// everything else is handed to the standard parser rather than reimplemented.
func ParseDuration(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty duration")
	}

	upper := strings.ToUpper(trimmed)
	for _, unit := range durationUnits {
		if !strings.HasSuffix(upper, unit.suffix) {
			continue
		}
		digits := strings.TrimSpace(upper[:len(upper)-len(unit.suffix)])
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		if value <= 0 {
			return 0, fmt.Errorf("duration %q must be positive", s)
		}
		if value > int64(1<<62)/int64(unit.factor) {
			return 0, fmt.Errorf("duration %q overflows", s)
		}
		return time.Duration(value) * unit.factor, nil
	}

	duration, err := time.ParseDuration(strings.ToLower(trimmed))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: want a value such as 7d, 12h, or 90m", s)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return duration, nil
}
