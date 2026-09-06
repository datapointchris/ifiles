package cmd

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

// defaultRowCap bounds every listing. All three of them cross the tunnel, and a
// listing that reads over a network bounds its default rather than promising a
// whole set whose size the far end decides.
//
// 100 is the published ceiling on the REST contracts this most resembles, and
// it is high enough that an ordinary directory or search arrives whole. The cap
// is there for the pathological set, not for the usual one.
const defaultRowCap = 100

// limitFlag is an int the parser can refuse. A row count below zero is not a
// count, and pflag's own int accepts one and hands it to a slice expression.
//
// Zero is a count, and it means no rows — the way `head -n 0` means it. Nothing
// here reads it as a request for everything.
type limitFlag struct {
	rows int
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
		return errors.New("a row count cannot be negative; the smallest cap is 0")
	}
	l.rows = rows
	return nil
}

// String is what pflag stores as the flag's default and prints on the help
// screen, so the cap a caller gets without asking is on the screen where they
// would look for it.
func (l *limitFlag) String() string { return strconv.Itoa(l.rows) }

// Type is the word help prints after the flag name.
func (l *limitFlag) Type() string { return "int" }

// limited cuts rows down to the cap.
func limited[T any](rows []T, limit limitFlag) []T {
	if len(rows) <= limit.rows {
		return rows
	}
	return rows[:limit.rows]
}

// reachedCap reports whether the listing stopped at the cap rather than at the
// end of the data. A full page is the one screen that cannot say which it was,
// because the row count alone reads as the total.
func reachedCap(shown int, limit limitFlag) bool {
	return limit.rows > 0 && shown == limit.rows
}

// capNotice writes that hint to stderr, which leaves --json and a pipe holding
// only rows.
func capNotice(cmd *cobra.Command, shown int, limit limitFlag) {
	if reachedCap(shown, limit) {
		infof(cmd, "\nStopped at the %d-row cap; -n raises it.", limit.rows)
	}
}

// registerLimit attaches the row cap to a listing command. The name, the
// shorthand, the default and the floor are written once here, so every verb
// that takes a cap reads the same value the same way.
func registerLimit(command *cobra.Command, limit *limitFlag, usage string) {
	limit.rows = defaultRowCap
	command.Flags().VarP(limit, "limit", "n", usage)
}
