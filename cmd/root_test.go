package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
)

// runLine drives a command line through execute, the path the shipped binary
// takes. goclikit resolves the command from os.Args, so the line goes there
// rather than through SetArgs.
func runLine(t *testing.T, args ...string) error {
	t.Helper()
	original := os.Args
	os.Args = append([]string{"ifiles"}, args...)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	t.Cleanup(func() {
		os.Args = original
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	return execute(context.Background(), autoupdate.Config{Suppress: true})
}

// Cobra parses flags and answers --help before it validates a group's
// arguments, so without the namespace marking a mistyped subcommand was
// reported as the flag after it, or answered with the group's help and exit 0.
func TestAnUnknownWordInAGroupIsRefusedWhateverFollowsIt(t *testing.T) {
	for _, args := range [][]string{
		{"shares", "bogus"},
		{"shares", "bogus", "--json"},
		{"shares", "bogus", "--help"},
		{"auth", "bogus"},
		{"auth", "bogus", "--json"},
		{"auth", "bogus", "--help"},
	} {
		err := runLine(t, args...)
		if !errors.Is(err, goclikit.ErrUsage) || !strings.Contains(err.Error(), `unknown command "bogus"`) {
			t.Errorf("%v answered %v, want a usage error refusing \"bogus\"", args, err)
		}
	}
}

func TestABareGroupShowsItsHelp(t *testing.T) {
	for _, group := range []string{"shares", "auth"} {
		if err := runLine(t, group); err != nil {
			t.Errorf("bare %q failed: %v", group, err)
		}
	}
}

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
