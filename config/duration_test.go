package config

import (
	"testing"
	"time"
)

func TestParseDurationUnderstandsDaysAndWeeks(t *testing.T) {
	t.Parallel()

	cases := map[string]time.Duration{
		// The two units time.ParseDuration does not have, which are the two a
		// share link is actually set in.
		"7d":  7 * 24 * time.Hour,
		"1d":  24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
		"7D":  7 * 24 * time.Hour,
		"12h": 12 * time.Hour,
		"90m": 90 * time.Minute,
		"30s": 30 * time.Second,
		// Delegated, so the compound forms come for free.
		"1h30m": 90 * time.Minute,
	}

	for input, want := range cases {
		got, err := ParseDuration(input)
		if err != nil {
			t.Errorf("ParseDuration(%q) returned error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseDurationRejectsWhatWouldSilentlyMisbehave(t *testing.T) {
	t.Parallel()

	// Zero and negative both matter: the server adds whatever it is given to the
	// current time, so either would produce a link that expired before it existed
	// rather than an error.
	for _, input := range []string{"", "   ", "0d", "0h", "-3d", "-1h", "7", "d", "soon", "1.5d"} {
		if got, err := ParseDuration(input); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want an error", input, got)
		}
	}
}
