package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v3/config"
	"github.com/datapointchris/ifiles/v3/filebrowser"
)

var (
	lsJSON  bool
	lsLimit int
	lsAll   bool
	lsLong  bool
)

var listCmd = &cobra.Command{
	Use:        "list [path]",
	GroupID:    groupRead,
	SuggestFor: []string{"ls", "dir"},
	Short:      "List a remote directory",
	Long: `Lists the contents of a remote directory, folders first.

The path is absolute from the source root; omitted, it lists the root. Hidden
entries are excluded unless -a is given, matching the shell rather than the web
UI's per-account setting.`,
	Example: `  ifiles list                     the source root
  ifiles list /photos/2026        one directory
  ifiles list /photos -l          with sizes and modification times
  ifiles list /photos --json      for a script`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		remotePath := "/"
		if len(args) == 1 {
			remotePath = filebrowser.CleanPath(args[0])
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		listing, err := client.List(ctx, remotePath)
		if err != nil {
			if filebrowser.IsNotFound(err) {
				return fmt.Errorf("%s does not exist on the server", remotePath)
			}
			return err
		}

		entries := listing.Entries()
		if !lsAll {
			visible := entries[:0]
			for _, entry := range entries {
				if !entry.Hidden {
					visible = append(visible, entry)
				}
			}
			entries = visible
		}
		if lsLimit > 0 && len(entries) > lsLimit {
			entries = entries[:lsLimit]
		}

		if lsJSON {
			return emitJSON(cmd, entries)
		}

		if len(entries) == 0 {
			infof(cmd, "%s is empty.", remotePath)
			return nil
		}

		if !lsLong {
			for _, entry := range entries {
				outf(cmd, "%s", name(entry))
			}
			return nil
		}

		table := newTable(cmd.OutOrStdout())
		writef(table, "SIZE\tMODIFIED\tNAME\n")
		for _, entry := range entries {
			size := config.FormatSize(entry.Size)
			if entry.IsDir() {
				size = "-"
			}
			writef(table, "%s\t%s\t%s\n", size, entry.Modified.Local().Format("2006-01-02 15:04"), name(entry))
		}
		return table.Flush()
	},
}

// name marks directories with a trailing slash, which is the handle a caller
// pastes back into the next command — `ifiles list /photos/2026/` works, and the
// slash is what says it will.
func name(entry filebrowser.Item) string {
	if entry.IsDir() {
		return entry.Name + "/"
	}
	return entry.Name
}

func init() {
	listCmd.Flags().BoolVar(&lsJSON, "json", false, "Output entries as JSON to stdout")
	listCmd.Flags().IntVarP(&lsLimit, "limit", "n", 0, "maximum entries to list (0 for all)")
	listCmd.Flags().BoolVarP(&lsAll, "all", "a", false, "include hidden entries")
	listCmd.Flags().BoolVarP(&lsLong, "long", "l", false, "show size and modification time")
	rootCmd.AddCommand(listCmd)
}
