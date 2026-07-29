package config

import "testing"

func TestParseSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  int64
	}{
		{"1024", 1024},
		{"1B", 1},
		{"8K", 8 << 10},
		{"32MiB", 32 << 20},
		{"32mib", 32 << 20},
		{" 32 MiB ", 32 << 20},
		{"1GiB", 1 << 30},
		// Decimal and binary are deliberately distinct: the value is compared
		// against a hard 100 MB transfer cap where the 4.8% gap decides whether the
		// request succeeds.
		{"1MB", 1000 * 1000},
		{"1MiB", 1 << 20},
	}

	for _, tc := range cases {
		got, err := ParseSize(tc.input)
		if err != nil {
			t.Errorf("ParseSize(%q) returned error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParseSizeRejectsBadInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "  ", "0", "-5", "-5MiB", "MiB", "1.5MiB", "1XB", "1 2 MiB"} {
		if _, err := ParseSize(input); err == nil {
			t.Errorf("ParseSize(%q) succeeded, want an error", input)
		}
	}
}

func TestChunksDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	size, err := cfg.Chunks()
	if err != nil {
		t.Fatalf("Chunks() returned error: %v", err)
	}
	if size != DefaultChunkSize {
		t.Errorf("Chunks() = %d, want the default %d", size, DefaultChunkSize)
	}
}

func TestChunksStaysUnderTheTunnelCap(t *testing.T) {
	t.Parallel()

	// Cloudflare's free tier rejects a request body over 100 MB with a 413 that no
	// retry clears, so a default at or above the cap would make every large upload
	// fail. This guards the constant, not the parser.
	const cap = 100 * 1000 * 1000
	if DefaultChunkSize >= cap {
		t.Errorf("DefaultChunkSize = %d, which is not below the %d byte request cap", DefaultChunkSize, cap)
	}
}

func TestFormatSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		bytes int64
		want  string
	}{
		{512, "512B"},
		{2048, "2.0K"},
		{5 << 20, "5.0M"},
		{3 << 30, "3.0G"},
	}

	for _, tc := range cases {
		if got := FormatSize(tc.bytes); got != tc.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
