package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/datapointchris/ifiles/filebrowser"
)

// listingsWithARowCap is the oracle the tree walk below compares against, and
// it is written by hand on purpose. A walk that both collects and checks through
// Lookup("limit") cannot see a listing that lost its cap: the flag leaves the
// collection and the check together, and the comparison still holds. This table
// is decided somewhere that lookup cannot reach, so dropping registerLimit from
// a verb fails here instead of passing quietly.
//
// The sentence is spelled out rather than built the way registerLimit builds
// it, because it is the one string every caller reads and a test that composes
// it the same way would agree with any mistake in the composition.
var listingsWithARowCap = map[string]string{
	"ifiles list":        "Maximum number of entries to show (0 shows none)",
	"ifiles search":      "Maximum number of matches to show (0 shows none)",
	"ifiles shares list": "Maximum number of share links to show (0 shows none)",
}

// newListing is a bare command carrying a row cap, so the flag can be parsed
// and printed without reaching into one of the real listings and mutating the
// package-level flag every other test reads.
func newListing() (*cobra.Command, *limitFlag) {
	var limit limitFlag
	command := &cobra.Command{Use: "listing"}
	registerLimit(command, &limit, "rows")
	return command, &limit
}

// A cap of zero is a request a caller can make, and `head -n 0` is where they
// learned to make it. No cap reads as a request for everything.
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
	err := command.Flags().Parse([]string{"--limit=-1"})
	if err == nil {
		t.Fatal("--limit=-1 parsed, want a usage error")
	}
	// The noun comes from the command rather than from the flag's own
	// vocabulary, so a caller reads the same word here and on the help screen.
	if !strings.Contains(err.Error(), "rows") {
		t.Errorf("the refusal does not name what was counted: %v", err)
	}
	if limit.set {
		t.Errorf("a refused cap was recorded as %+v, want the flag left unset", limit)
	}
	if got := limited([]int{1, 2, 3}, *limit); len(got) != 3 {
		t.Errorf("a refused cap kept %d rows, want all 3", len(got))
	}
}

// pflag appends a flag's default to its usage line, and there is no default
// here to append: an uncapped listing shows every row. A number on that line
// would name a cap that is not applied, and the number pflag reaches for first
// is 0 — which is the cap that means no rows at all.
//
// The line also has to say what 0 does, on the release that changes what 0
// does. Nothing else on the screen carries it.
func TestAnUncappedListingPrintsNoDefault(t *testing.T) {
	t.Parallel()

	const written = "Maximum number of rows to show (0 shows none)"

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
// nothing.
//
// The cap has to win over every other narrowing, because widening any of those
// while it stands still shows nothing. A remedy offered under a zero cap is a
// remedy the reader can follow and get the same empty screen from.
func TestAnEmptyListingBlamesTheCapBeforeAnyOtherNarrowing(t *testing.T) {
	t.Parallel()

	zero := limitFlag{rows: 0, set: true}
	three := limitFlag{rows: 3, set: true}
	none := limitFlag{}

	t.Run("entries", func(t *testing.T) {
		cases := map[string]struct {
			limit             limitFlag
			present, unhidden int
			want              listEmpty
		}{
			"nothing on the server":       {none, 0, 0, listNothingThere},
			"every entry hidden":          {none, 4, 0, listAllHidden},
			"one entry, all of it hidden": {none, 1, 0, listAllHidden},
			"capped at zero":              {zero, 4, 4, listCappedToNothing},
			"hidden, then capped":         {zero, 4, 2, listCappedToNothing},
			// The cap wins over the hidden filter, because -a cannot show a row
			// while --limit 0 stands.
			"hidden and capped at zero": {zero, 4, 0, listCappedToNothing},
			"empty and capped at zero":  {zero, 0, 0, listCappedToNothing},
			// No narrowing this command knows about accounts for it.
			"nothing accounts for it": {three, 4, 4, listUnaccounted},
		}
		for name, tc := range cases {
			if got := listingEmptiness(tc.limit, tc.present, tc.unhidden); got != tc.want {
				t.Errorf("%s: listingEmptiness(%+v, %d, %d) = %d, want %d",
					name, tc.limit, tc.present, tc.unhidden, got, tc.want)
			}
		}
	})

	t.Run("share links", func(t *testing.T) {
		cases := map[string]struct {
			limit limitFlag
			scope string
			found int
			want  sharesEmpty
		}{
			"account has none":  {none, "", 0, sharesNoneOnTheAccount},
			"none for the path": {none, "/photos", 0, sharesNoneForThePath},
			"capped at zero":    {zero, "", 3, sharesCappedToNothing},
			// The cap wins over the path, because widening to the whole account
			// cannot show a row while --limit 0 stands.
			"capped under the path":   {zero, "/photos", 3, sharesCappedToNothing},
			"capped and none found":   {zero, "/photos", 0, sharesCappedToNothing},
			"nothing accounts for it": {three, "", 3, sharesUnaccounted},
		}
		for name, tc := range cases {
			if got := sharesEmptiness(tc.limit, tc.scope, tc.found); got != tc.want {
				t.Errorf("%s: sharesEmptiness(%+v, %q, %d) = %d, want %d",
					name, tc.limit, tc.scope, tc.found, got, tc.want)
			}
		}
	})

	t.Run("matches", func(t *testing.T) {
		cases := map[string]struct {
			limit   limitFlag
			matched int
			want    matchesEmpty
		}{
			"index found none":        {none, 0, matchesNoneFound},
			"capped at zero":          {zero, 7, matchesCappedToNothing},
			"capped and none found":   {zero, 0, matchesCappedToNothing},
			"nothing accounts for it": {three, 7, matchesUnaccounted},
		}
		for name, tc := range cases {
			if got := matchesEmptiness(tc.limit, tc.matched); got != tc.want {
				t.Errorf("%s: matchesEmptiness(%+v, %d) = %d, want %d",
					name, tc.limit, tc.matched, got, tc.want)
			}
		}
	})
}

// Classifying is only half of it. The half a reader sees is the sentence, and
// nothing pins that a reason reaches its own: swapping two arms of a renderer
// leaves the classifier's tests green while every reader is told the wrong
// thing.
//
// Distinctness is what is asserted, not the wording. Two reasons rendering
// alike means one of them is unreportable, whatever the words are, and the
// assertion survives any rewrite of them.
func TestEveryReasonRendersItsOwnSentence(t *testing.T) {
	t.Parallel()

	t.Run("entries", func(t *testing.T) {
		seen := map[string]listEmpty{}
		for _, reason := range listEmptyReasons {
			sentence := emptyListing("/photos", reason)
			if sentence == "" {
				t.Errorf("reason %d renders nothing", reason)
			}
			if first, repeat := seen[sentence]; repeat {
				t.Errorf("reasons %d and %d both render %q", first, reason, sentence)
			}
			seen[sentence] = reason
		}
	})

	t.Run("share links", func(t *testing.T) {
		seen := map[string]sharesEmpty{}
		for _, reason := range sharesEmptyReasons {
			sentence := emptyShares("/photos", reason)
			if sentence == "" {
				t.Errorf("reason %d renders nothing", reason)
			}
			if first, repeat := seen[sentence]; repeat {
				t.Errorf("reasons %d and %d both render %q", first, reason, sentence)
			}
			seen[sentence] = reason
		}
	})

	t.Run("matches", func(t *testing.T) {
		request := filebrowser.SearchRequest{Query: "invoice", Scope: "/documents"}
		seen := map[string]matchesEmpty{}
		for _, reason := range matchesEmptyReasons {
			sentence := emptyMatches(request, reason)
			if sentence == "" {
				t.Errorf("reason %d renders nothing", reason)
			}
			if first, repeat := seen[sentence]; repeat {
				t.Errorf("reasons %d and %d both render %q", first, reason, sentence)
			}
			seen[sentence] = reason
		}
	})
}

// An empty state is a help surface, so it ends by naming a command rather than
// leaving the reader to guess the widening. Only the answers that have a wider
// question to offer owe one: an unscoped search has already asked the whole
// index, and there is nothing further to point at.
func TestAnEmptyStateWithAWiderQuestionNamesIt(t *testing.T) {
	t.Parallel()

	scoped := filebrowser.SearchRequest{Query: "invoice", Scope: "/documents"}
	cases := map[string]struct{ sentence, wants string }{
		"a directory of hidden entries": {emptyListing("/photos", listAllHidden), "ifiles list /photos -a"},
		"a path with no links":          {emptyShares("/photos", sharesNoneForThePath), "ifiles shares list"},
		"a scoped search":               {emptyMatches(scoped, matchesNoneFound), "ifiles search invoice"},
	}

	for name, tc := range cases {
		if !strings.Contains(tc.sentence, tc.wants) {
			t.Errorf("%s: %q does not name %q", name, tc.sentence, tc.wants)
		}
	}
}

// A page of rows reads as the whole set, so a listing that was cut says by how
// much and names the flag that widens it. The gate is the count that was cut
// rather than the cap being reached, so a listing holding exactly the cap is
// not reported as truncated.
func TestOnlyAShortenedListingIsReported(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		shown, before int
		want          bool
	}{
		"cut from more":        {3, 21, true},
		"exactly the cap":      {3, 3, false},
		"nothing cut":          {21, 21, false},
		"emptied":              {0, 21, false},
		"empty on both counts": {0, 0, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			command := &cobra.Command{Use: "listing"}
			var stderr strings.Builder
			command.SetErr(&stderr)

			shortened(command, tc.shown, tc.before, limitFlag{rows: tc.shown, set: true, noun: "entries"})

			if reported := stderr.Len() > 0; reported != tc.want {
				t.Errorf("shortened(%d, %d) reported %t, want %t: %q",
					tc.shown, tc.before, reported, tc.want, stderr.String())
			}
		})
	}
}

// One flag meaning two things inside one CLI is what this pins. A listing added
// later is where a second reading comes back: a plain int flag takes a negative
// straight into a slice expression, and a zero default reads as "everything" to
// whoever writes the truncation. The help screen shows no difference either way.
//
// The two directions are checked against different oracles. Every listing that
// owes a cap is named in listingsWithARowCap, and every --limit found in the
// tree has to be one of them — so a cap that goes missing and a cap that
// appears under another name both fail.
func TestEveryListingSpellsItsRowCapTheSameWay(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, command := range descendants(rootCmd) {
		flag := command.Flags().Lookup("limit")
		if flag == nil {
			continue
		}
		path := command.CommandPath()
		found[path] = true

		t.Run(path, func(t *testing.T) {
			usage, owed := listingsWithARowCap[path]
			if !owed {
				t.Fatalf("%s declares --limit and is not in listingsWithARowCap", path)
			}
			if flag.Usage != usage {
				t.Errorf("--limit reads %q, want %q", flag.Usage, usage)
			}
			if _, ok := flag.Value.(*limitFlag); !ok {
				t.Errorf("--limit binds to a %T, want a *limitFlag", flag.Value)
			}
			if flag.Shorthand != "n" {
				t.Errorf("--limit has shorthand %q, want %q", flag.Shorthand, "n")
			}
			if flag.DefValue != "" {
				t.Errorf("--limit defaults to %q, want no default", flag.DefValue)
			}
		})
	}

	for path := range listingsWithARowCap {
		if !found[path] {
			t.Errorf("%s owes a row cap and declares no --limit", path)
		}
	}
}

// The floor belongs to every integer flag, not to the one that prompted it. A
// negative is never a number of things, and each of these reaches something
// that acts on it: a slice expression, a transfer deadline, a durable public
// link whose cap nothing on screen reports.
//
// The walk is what makes this hold for the next integer flag somebody adds.
// pflag's own int accepts a negative, so a flag declared with IntVar rather
// than through one of the floored values fails here on the day it lands.
func TestEveryIntegerFlagRefusesANegative(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, command := range descendants(rootCmd) {
		command.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Value.Type() != "int" {
				return
			}
			checked++
			// Set is called rather than Parse, so a refusal cannot leave the
			// package-level flag this command really uses holding -1.
			if err := flag.Value.Set("-1"); err == nil {
				t.Errorf("%s --%s accepted -1, and now reads %s",
					command.CommandPath(), flag.Name, flag.Value)
			}
		})
	}

	// Three today: --limit, --downloads, --timeout. The count is not pinned,
	// because a fourth is exactly what this walk exists to reach.
	if checked == 0 {
		t.Fatal("no integer flag was found, so this test pins nothing")
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
