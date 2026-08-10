package cmd

import (
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v3/config"
	"github.com/datapointchris/ifiles/v3/filebrowser"
)

var (
	searchJSON     bool
	searchLimit    int
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
		if searchLimit > 0 && len(results) > searchLimit {
			results = results[:searchLimit]
		}

		if searchJSON {
			return emitJSON(cmd, results)
		}

		if len(results) == 0 {
			infof(cmd, "No matches.")
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

func init() {
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output matches as JSON to stdout")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 0, "maximum matches to show (0 for all)")
	searchCmd.Flags().BoolVar(&searchWildcard, "glob", false, "treat the query as a glob pattern")
	rootCmd.AddCommand(searchCmd)
}
