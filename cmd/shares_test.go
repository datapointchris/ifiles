package cmd

import "testing"

// TestShareHashAcceptsEveryFormAShareIsKnownBy is what makes the listing usable
// as input. The table prints the share URL rather than the hash, so a row that
// could not be pasted back into `shares delete` would be a row you can read and not
// act on.
func TestShareHashAcceptsEveryFormAShareIsKnownBy(t *testing.T) {
	t.Parallel()

	const hash = "T7bQ3xkLm2pR8vN4wZ"
	cases := map[string]string{
		"bare hash":                hash,
		"share URL":                "https://files.example.com/public/share/" + hash,
		"share URL, slash":         "https://files.example.com/public/share/" + hash + "/",
		"surrounded by whitespace": "  " + hash + "  ",
		// The download URL carries the hash in the query string, on a route named
		// /public/api/resources/download — so splitting on the last slash would
		// answer "download" and revoke nothing.
		"download URL": "https://files.example.com/public/api/resources/download?hash=" + hash,
		"download URL with token": "https://files.example.com/public/api/resources/download?hash=" +
			hash + "&token=abc.def",
	}

	for name, argument := range cases {
		got, err := shareHash(argument)
		if err != nil {
			t.Errorf("shareHash(%s) returned error: %v", name, err)
			continue
		}
		if got != hash {
			t.Errorf("shareHash(%s) = %q, want %q", name, got, hash)
		}
	}
}

func TestShareHashRejectsAnEmptyArgument(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"", "   ", "/"} {
		if got, err := shareHash(argument); err == nil {
			t.Errorf("shareHash(%q) = %q, want an error", argument, got)
		}
	}
}
