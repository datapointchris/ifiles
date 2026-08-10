package cmd

import (
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/config"
)

var sourcesJSON bool

var sourcesCmd = &cobra.Command{
	Use:     "sources",
	GroupID: groupAuth,
	Short:   "List the sources configured on the server",
	Long: `Lists the sources this account can reach.

Quantum is multi-source and every resource route takes a source name, so this is
the command that tells you what --source will accept. The configured default is
marked.`,
	Example: `  ifiles sources
  ifiles sources --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, cfg, err := newClient()
		if err != nil {
			return err
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		sources, err := client.Sources(ctx)
		if err != nil {
			return err
		}

		if sourcesJSON {
			return emitJSON(cmd, sources)
		}

		table := newTable(cmd.OutOrStdout())
		writef(table, "SOURCE\tSTATUS\tFILES\tDIRS\tUSED\tREAD-ONLY\tDEFAULT\n")
		for _, source := range sources {
			isDefault := ""
			if source.Name == cfg.Source {
				isDefault = "yes"
			}
			writef(table, "%s\t%s\t%d\t%d\t%s\t%t\t%s\n",
				source.Name, source.Status, source.NumFiles, source.NumDirs,
				config.FormatSize(int64(source.Used)), source.ReadOnly, isDefault)
		}
		return table.Flush()
	},
}

func init() {
	sourcesCmd.Flags().BoolVar(&sourcesJSON, "json", false, "Output sources as JSON to stdout")
	rootCmd.AddCommand(sourcesCmd)
}
