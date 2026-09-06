package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// parseCount reads a whole number of things and refuses a negative one. It
// takes the noun so the message names what the caller was counting, which is
// the word their command line and the help screen both use.
//
// pflag prefixes what this returns with the flag and the argument, so the
// sentence completes "invalid argument %q for %q flag: ".
func parseCount(raw, noun string) (int, error) {
	count, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s are counted in whole numbers", noun)
	}
	if count < 0 {
		// What zero means is the flag's own business — no rows for a cap, an
		// uncapped link for a download limit — so the floor is stated without
		// it and each flag's help carries the rest.
		return 0, fmt.Errorf("a number of %s cannot be negative; the smallest is 0", noun)
	}
	return count, nil
}

// limitFlag holds the three answers a row cap has: no cap at all, a cap of
// zero, and a cap above zero. A plain int holds only two, because its zero has
// to serve as both "no rows" and "the flag was never typed" — and "no rows" is
// something a caller can ask for, the way `head -n 0` asks for it. So absence
// gets a field of its own beside the count.
//
// No listing here bounds its default. Each one asks the server for a whole set
// and gets it in a single response, so a cap could only truncate a table that
// has already arrived. What a caller does not ask to narrow, they see.
type limitFlag struct {
	rows int
	set  bool
	noun string
}

// Set is where the floor lives, because the parser is the only layer that runs
// before the server is called: a caller who typed a cap the command cannot use
// learns it without a round trip. Cobra routes the failure through its flag
// error handler, so it exits 2 as a typing mistake rather than 1 as a failed
// run.
func (l *limitFlag) Set(raw string) error {
	rows, err := parseCount(raw, l.countedNoun())
	if err != nil {
		return err
	}
	l.rows, l.set = rows, true
	return nil
}

// countedNoun falls back to "rows" for a flag built outside registerLimit,
// which is the only way the noun goes unset.
func (l *limitFlag) countedNoun() string {
	if l.noun == "" {
		return "rows"
	}
	return l.noun
}

// String is what pflag stores as the flag's default and prints on the help
// screen. An uncapped flag answers with the empty string, which pflag reads as
// a zero value and prints nothing for — any number there would name a cap that
// is not applied, and 0 would name the one that means no rows at all.
func (l *limitFlag) String() string {
	if !l.set {
		return ""
	}
	return strconv.Itoa(l.rows)
}

// Type is the word help prints after the flag name.
func (l *limitFlag) Type() string { return "int" }

// asksForNothing reports a cap of zero, which is the caller asking for no rows.
// It voids every other narrowing, because widening any of those still shows
// nothing while this one stands.
func (l *limitFlag) asksForNothing() bool { return l.set && l.rows == 0 }

// limited cuts rows down to the cap. An uncapped flag returns every row, since
// "all" is the absence of a cap rather than a number a cap could carry.
func limited[T any](rows []T, limit limitFlag) []T {
	if !limit.set || len(rows) <= limit.rows {
		return rows
	}
	return rows[:limit.rows]
}

// shortened writes the count that was cut, because a page of rows reads as the
// whole set and nothing else on screen says otherwise. It names the real total
// rather than a remainder, and ends with the flag that widens it.
//
// The gate is shown < before rather than shown == the cap, so a listing that
// happens to hold exactly the cap is not reported as truncated.
func shortened(cmd *cobra.Command, shown, before int, limit limitFlag) {
	if shown == 0 || shown >= before {
		return
	}
	infof(cmd, "\nShowing %d of %d %s; -n raises the cap.", shown, before, limit.countedNoun())
}

// registerLimit attaches the row cap to a listing command. The name, the
// shorthand, the floor and the sentence a reader sees are written once here, so
// a verb added later cannot spell any of them its own way. The noun is the only
// per-command part, and it reaches the help text and the parse error together.
func registerLimit(command *cobra.Command, limit *limitFlag, noun string) {
	limit.noun = noun
	usage := fmt.Sprintf("Maximum number of %s to show (0 shows none)", noun)
	command.Flags().VarP(limit, "limit", "n", usage)
}
