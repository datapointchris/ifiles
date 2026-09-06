package cmd

import (
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

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}
