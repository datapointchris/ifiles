package cmd

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

// limitFlag holds the three answers a row cap has: no cap at all, a cap of
// zero, and a cap above zero. A plain int holds only two, because its zero has
// to serve as both "no rows" and "the flag was never typed" — and "no rows" is
// something a caller can ask for, the way `head -n 0` asks for it. So absence
// gets a field of its own beside the count.
type limitFlag struct {
	rows int
	set  bool
}

// Set is where the floor lives, because the parser is the only layer that runs
// before the server is called: a caller who typed a cap the command cannot use
// learns it without a round trip. Cobra routes the failure through its flag
// error handler, so it exits 2 as a typing mistake rather than 1 as a failed
// run.
func (l *limitFlag) Set(raw string) error {
	rows, err := strconv.Atoi(raw)
	if err != nil {
		return errors.New("a row count is a whole number")
	}
	if rows < 0 {
		return errors.New("a row count cannot be negative; omit --limit for every row")
	}
	l.rows, l.set = rows, true
	return nil
}

// String is what pflag stores as the flag's default and prints on the help
// screen. An uncapped flag answers with the empty string, which pflag reads as
// a zero value and prints nothing for — a "(default 0)" there would tell the
// reader that zero is how you ask for everything.
func (l *limitFlag) String() string {
	if !l.set {
		return ""
	}
	return strconv.Itoa(l.rows)
}

// Type is the word help prints after the flag name.
func (l *limitFlag) Type() string { return "int" }

// limited cuts rows down to the cap. An uncapped flag returns every row, since
// "all" is the absence of a cap rather than a number a cap could carry.
func limited[T any](rows []T, limit limitFlag) []T {
	if !limit.set || len(rows) <= limit.rows {
		return rows
	}
	return rows[:limit.rows]
}

// registerLimit attaches the row cap to a listing command. The name, the
// shorthand and the floor are written once here, so every verb that takes a cap
// reads the same value the same way.
func registerLimit(command *cobra.Command, limit *limitFlag, usage string) {
	command.Flags().VarP(limit, "limit", "n", usage)
}
