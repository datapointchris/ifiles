package cmd

import "testing"

// countSuggestions tallies what the root command offers for a mistyped word.
func countSuggestions(typed string) (map[string]int, []string) {
	offered := rootCmd.SuggestionsFor(typed)
	counts := make(map[string]int, len(offered))
	for _, name := range offered {
		counts[name]++
	}
	return counts, offered
}

// Cobra reaches these on edit distance alone, so naming them in SuggestFor as
// well would print the command twice under "Did you mean this?". This table is
// what says the distance guess still covers them: a cobra upgrade that narrows
// it, or a smaller SuggestionsMinimumDistance, drops the suggestion entirely and
// fails here rather than in a user's terminal.
func TestAUnixNameStillReachesItsCommand(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"ls", "list"},
		{"del", "delete"},
		{"cp", "copy"},
		{"mv", "move"},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			counts, offered := countSuggestions(tc.typed)
			if counts[tc.want] != 1 {
				t.Errorf("%q offered %q %d times, want once: %v", tc.typed, tc.want, counts[tc.want], offered)
			}
		})
	}
}

// Every word a command claims must reach it, exactly once. A word cobra already
// reaches on distance or prefix is appended a second time when SuggestFor names
// it too, which is the duplicate this catches for any alias added later.
func TestADeclaredAliasReachesItsCommandExactlyOnce(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		for _, alias := range cmd.SuggestFor {
			t.Run(alias, func(t *testing.T) {
				counts, offered := countSuggestions(alias)
				if counts[cmd.Name()] != 1 {
					t.Errorf("%q offered %q %d times, want once: %v", alias, cmd.Name(), counts[cmd.Name()], offered)
				}
				for name, n := range counts {
					if n > 1 {
						t.Errorf("%q offered %q %d times: %v", alias, name, n, offered)
					}
				}
			})
		}
	}
}
