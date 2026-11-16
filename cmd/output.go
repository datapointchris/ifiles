package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// writef discards the write error deliberately: a failed write to stdout or
// stderr cannot be reported anywhere, because the stream that would carry the
// report is the one that failed. Doing it here keeps the explicit discard in one
// place instead of at every call site.
func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// outf writes machine-consumable output to stdout and nothing else, so a caller
// parsing it never has to filter diagnostics out of the stream.
func outf(cmd *cobra.Command, format string, args ...any) {
	writef(cmd.OutOrStdout(), format+"\n", args...)
}

// infof writes progress, confirmations, and warnings to stderr.
func infof(cmd *cobra.Command, format string, args ...any) {
	writef(cmd.ErrOrStderr(), format+"\n", args...)
}

func emitJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// table aligns columns into a buffer over w. A tabwriter writes each cell and
// each run of padding as a write of its own, which on an unbuffered stdout is a
// syscall apiece, so the buffer collects them and Flush sends the table at once.
type table struct {
	*tabwriter.Writer
	out *bufio.Writer
}

func newTable(w io.Writer) *table {
	out := bufio.NewWriter(w)
	return &table{Writer: tabwriter.NewWriter(out, 0, 0, 2, ' ', 0), out: out}
}

// Flush aligns the buffered rows, then writes them to the underlying writer.
func (t *table) Flush() error {
	if err := t.Writer.Flush(); err != nil {
		return err
	}
	return t.out.Flush()
}
