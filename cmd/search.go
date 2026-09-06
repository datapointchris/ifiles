package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/config"
	"github.com/datapointchris/ifiles/filebrowser"
)

var (
	searchJSON     bool
	searchLimit    limitFlag
	searchWildcard bool
)

var searchCmd = &cobra.Command{
	Use:     "search <query> [path]",
	GroupID: groupRead,
	Short:   "Find files by name",
	Long: `Searches the server's index by filename.

This runs against the index the server already maintains, so it is fast over a
large tree and does not walk anything. A second argument narrows the search to a
subtree.

The server enforces a minimum query length and rejects anything shorter, which is
why a one-character search reports an error rather than everything.`,
	Example: `  ifiles search invoice              anywhere in the source
  ifiles search invoice /documents   within one subtree
  ifiles search '*.cr2' --glob       by extension
  ifiles search backup --json        for a script`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		request := filebrowser.SearchRequest{Query: args[0], Wildcard: searchWildcard}
		if len(args) == 2 {
			request.Scope = filebrowser.CleanPath(args[1])
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		results, err := client.Search(ctx, request)
		if err != nil {
			return err
		}
		matched := len(results)
		results = limited(results, searchLimit)

		// Above the --json return, so a caller reading the machine door is told
		// why the set is empty or short rather than being handed two bytes that
		// could mean either. Both go to stderr, and shortened is deferred so it
		// lands under what it describes.
		if len(results) == 0 {
			reason := matchesEmptiness(searchLimit, matched)
			infof(cmd, "%s", emptyMatches(request, reason))
		}
		defer shortened(cmd, len(results), matched, searchLimit)

		if searchJSON {
			return emitJSON(cmd, results)
		}
		if len(results) == 0 {
			return nil
		}

		table := newTable(cmd.OutOrStdout())
		writef(table, "SIZE\tPATH\n")
		for _, result := range results {
			size := config.FormatSize(result.Size)
			if result.IsDir() {
				size = "-"
			}
			writef(table, "%s\t%s\n", size, result.Path)
		}
		return table.Flush()
	},
}

// matchesEmpty names what left a search with no rows. Its members are this
// command's own, so a reason belonging to another verb cannot reach the
// renderer below.
type matchesEmpty int

const (
	// matchesUnaccounted is the zero value, so a narrowing added later and left
	// unclassified answers with the sentence that keeps the reader looking.
	matchesUnaccounted matchesEmpty = iota
	matchesNoneFound
	matchesCappedToNothing
)

// matchesEmptyReasons is every member, for the test that walks them.
var matchesEmptyReasons = []matchesEmpty{
	matchesUnaccounted, matchesNoneFound, matchesCappedToNothing,
}

// matchesEmptiness takes the cap rather than inferring it from the count, and
// asks it first because a cap of zero voids every widening the other answers
// could name.
func matchesEmptiness(limit limitFlag, matched int) matchesEmpty {
	switch {
	case limit.asksForNothing():
		return matchesCappedToNothing
	case matched == 0:
		return matchesNoneFound
	default:
		return matchesUnaccounted
	}
}

// emptyMatches names the widening command where one exists. A scoped search has
// a wider search to offer; an unscoped one has already asked the whole index,
// so there is nothing further to point at.
func emptyMatches(request filebrowser.SearchRequest, reason matchesEmpty) string {
	const unaccounted = "Nothing to show, and no narrowing accounts for it."
	switch reason {
	case matchesCappedToNothing:
		return "--limit 0 asked for no matches."
	case matchesNoneFound:
		if request.Scope != "" {
			return fmt.Sprintf("No matches under %s; ifiles search %s searches the whole source.",
				request.Scope, request.Query)
		}
		return "No matches."
	case matchesUnaccounted:
		return unaccounted
	}
	// A member added to matchesEmpty and left unnamed above lands here, on the
	// answer that keeps the reader looking rather than the one that closes.
	return unaccounted
}

func init() {
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output matches as JSON to stdout")
	registerLimit(searchCmd, &searchLimit, "matches")
	searchCmd.Flags().BoolVar(&searchWildcard, "glob", false, "treat the query as a glob pattern")
	rootCmd.AddCommand(searchCmd)
}
