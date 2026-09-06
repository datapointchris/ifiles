package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newListing is a bare command carrying a row cap, so the flag can be parsed
// and printed without reaching into one of the real listings and mutating the
// package-level flag every other test reads.
func newListing() (*cobra.Command, *limitFlag) {
	var limit limitFlag
	command := &cobra.Command{Use: "listing"}
	registerLimit(command, &limit, "maximum rows to show (default all)")
	return command, &limit
}

// A cap of zero is a request a caller can make, and `head -n 0` is where they
// learned to make it. Everything is what an uncapped flag answers, and no
// number reaches that state.
func TestACapOfZeroAsksForNoRows(t *testing.T) {
	t.Parallel()

	rows := []string{"first", "second", "third"}
	cases := map[string]struct {
		limit limitFlag
		want  int
	}{
		"uncapped":          {limitFlag{}, 3},
		"zero":              {limitFlag{rows: 0, set: true}, 0},
		"under the count":   {limitFlag{rows: 2, set: true}, 2},
		"exactly the count": {limitFlag{rows: 3, set: true}, 3},
		"over the count":    {limitFlag{rows: 9, set: true}, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := limited(rows, tc.limit); len(got) != tc.want {
				t.Errorf("limited(%v, %+v) kept %d rows, want %d", rows, tc.limit, len(got), tc.want)
			}
		})
	}
}

// The parser is the only layer that runs before the request does, so a cap it
// cannot use is refused there rather than sliced with afterwards. A refused cap
// also has to leave the flag unset: recorded, it would truncate the listing it
// was rejected from.
func TestACapBelowZeroIsRefusedByTheParser(t *testing.T) {
	t.Parallel()

	command, limit := newListing()
	if err := command.Flags().Parse([]string{"--limit=-1"}); err == nil {
		t.Fatal("--limit=-1 parsed, want a usage error")
	}
	if limit.set {
		t.Errorf("a refused cap was recorded as %+v, want the flag left unset", limit)
	}
	if got := limited([]int{1, 2, 3}, *limit); len(got) != 3 {
		t.Errorf("a refused cap kept %d rows, want all 3", len(got))
	}
}

// pflag appends a flag's default to its usage line unless it reads that default
// as a zero value, and any cap it would print is a number the reader then takes
// for the uncapped answer. The line has to end where the written usage ends,
// with nothing added after it.
func TestAnUncappedListingPrintsNoDefault(t *testing.T) {
	t.Parallel()

	const written = "maximum rows to show (default all)"

	command, _ := newListing()
	usage := strings.TrimRight(command.Flags().FlagUsages(), " \n")

	if !strings.Contains(usage, "-n, --limit int") {
		t.Errorf("help does not spell the cap as an int flag:\n%s", usage)
	}
	if !strings.HasSuffix(usage, written) {
		t.Errorf("help appends something after the written usage:\n%s", usage)
	}
}

// A cap of zero empties a listing that had rows, and the empty state written
// for a genuinely bare listing then reports the wrong thing — that the
// directory holds nothing, that the account has no links, that the index found
// nothing. Each command has to tell its own narrowing apart from the population.
func TestAnEmptyListingNamesTheNarrowingThatEmptiedIt(t *testing.T) {
	t.Parallel()

	t.Run("entries", func(t *testing.T) {
		cases := map[string]struct {
			present, unhidden int
			want              emptyReason
		}{
			"nothing on the server":       {0, 0, populationEmpty},
			"every entry hidden":          {4, 0, hiddenFiltered},
			"capped at zero":              {4, 4, cappedToNothing},
			"hidden, then capped":         {4, 2, cappedToNothing},
			"one entry, all of it hidden": {1, 0, hiddenFiltered},
		}
		for name, tc := range cases {
			if got := listingEmptiness(tc.present, tc.unhidden); got != tc.want {
				t.Errorf("%s: listingEmptiness(%d, %d) = %s, want %s", name, tc.present, tc.unhidden, reasonName(got), reasonName(tc.want))
			}
		}
	})

	t.Run("share links", func(t *testing.T) {
		cases := map[string]struct {
			scope string
			found int
			want  emptyReason
		}{
			"account has none":      {"", 0, populationEmpty},
			"none for the path":     {"/photos", 0, scopeFiltered},
			"capped at zero":        {"", 3, cappedToNothing},
			"capped under the path": {"/photos", 3, cappedToNothing},
		}
		for name, tc := range cases {
			if got := sharesEmptiness(tc.scope, tc.found); got != tc.want {
				t.Errorf("%s: sharesEmptiness(%q, %d) = %s, want %s", name, tc.scope, tc.found, reasonName(got), reasonName(tc.want))
			}
		}
	})

	t.Run("matches", func(t *testing.T) {
		if got := matchesEmptiness(0); got != populationEmpty {
			t.Errorf("matchesEmptiness(0) = %s, want %s", reasonName(got), reasonName(populationEmpty))
		}
		if got := matchesEmptiness(7); got != cappedToNothing {
			t.Errorf("matchesEmptiness(7) = %s, want %s", reasonName(got), reasonName(cappedToNothing))
		}
	})
}

// One flag meaning two things inside one CLI is what this pins. A listing added
// later is where the older reading comes back: an int flag defaulting to 0
// truncates to nothing or to everything depending on which line wrote it, and
// the help screen shows no difference between the two.
func TestEveryListingSpellsItsRowCapTheSameWay(t *testing.T) {
	t.Parallel()

	capped := 0
	for _, command := range descendants(rootCmd) {
		flag := command.Flags().Lookup("limit")
		if flag == nil {
			continue
		}
		capped++

		t.Run(command.CommandPath(), func(t *testing.T) {
			if _, ok := flag.Value.(*limitFlag); !ok {
				t.Errorf("--limit binds to a %T, want a *limitFlag", flag.Value)
			}
			if flag.Shorthand != "n" {
				t.Errorf("--limit has shorthand %q, want %q", flag.Shorthand, "n")
			}
			if flag.DefValue != "" {
				t.Errorf("--limit defaults to %q, want no default", flag.DefValue)
			}
			if strings.Contains(flag.Usage, "0 for all") {
				t.Errorf("--limit is documented as %q", flag.Usage)
			}
		})
	}

	if capped == 0 {
		t.Fatal("no command declares --limit, so this test pins nothing")
	}
}

// reasonName spells an emptyReason so a failure names the branch that was taken
// rather than its position in the const block.
func reasonName(reason emptyReason) string {
	switch reason {
	case populationEmpty:
		return "populationEmpty"
	case hiddenFiltered:
		return "hiddenFiltered"
	case scopeFiltered:
		return "scopeFiltered"
	case cappedToNothing:
		return "cappedToNothing"
	default:
		return "unknown"
	}
}

// descendants walks the command tree, so a row cap is checked wherever it sits
// rather than against a hand-written list a new subcommand joins only if
// somebody remembers to add it.
func descendants(root *cobra.Command) []*cobra.Command {
	found := []*cobra.Command{root}
	for _, child := range root.Commands() {
		found = append(found, descendants(child)...)
	}
	return found
}
