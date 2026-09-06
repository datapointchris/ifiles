package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/config"
	"github.com/datapointchris/ifiles/filebrowser"
)

var (
	lsJSON  bool
	lsLimit limitFlag
	lsAll   bool
	lsLong  bool
)

var listCmd = &cobra.Command{
	Use:        "list [path]",
	GroupID:    groupRead,
	SuggestFor: []string{"dir"},
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
		present := len(entries)
		if !lsAll {
			visible := entries[:0]
			for _, entry := range entries {
				if !entry.Hidden {
					visible = append(visible, entry)
				}
			}
			entries = visible
		}
		unhidden := len(entries)
		entries = limited(entries, lsLimit)

		// Both notices sit above the --json return, so a caller reading the
		// machine door learns why a listing is empty or short rather than
		// receiving two bytes that could mean either. Both go to stderr, which
		// leaves stdout carrying rows alone. shortened is deferred so it lands
		// under what it describes in every rendering.
		if len(entries) == 0 {
			reason := listingEmptiness(lsLimit, present, unhidden)
			infof(cmd, "%s", emptyListing(remotePath, reason))
		}
		defer shortened(cmd, len(entries), unhidden, lsLimit)

		if lsJSON {
			return emitJSON(cmd, entries)
		}
		if len(entries) == 0 {
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

// listEmpty names what left a directory listing with no rows. Its members are
// this command's own, so a reason belonging to another verb cannot reach the
// renderer below: the compiler refuses it rather than a test catching it.
type listEmpty int

const (
	// listUnaccounted is first so the zero value is the answer that keeps the
	// reader looking. A narrowing added later and not classified here lands on
	// it, rather than on a sentence claiming the directory is bare.
	listUnaccounted listEmpty = iota
	listNothingThere
	listAllHidden
	listCappedToNothing
)

// listEmptyReasons is every member, for the test that walks them.
var listEmptyReasons = []listEmpty{
	listUnaccounted, listNothingThere, listAllHidden, listCappedToNothing,
}

// listingEmptiness names which narrowing emptied the listing, and takes the cap
// rather than inferring it from the counts either side.
//
// The cap is asked first because it voids the other answers. Following "-a
// lists them" while --limit 0 stands prints nothing again, so a remedy offered
// under a zero cap is a remedy that cannot work.
func listingEmptiness(limit limitFlag, present, unhidden int) listEmpty {
	switch {
	case limit.asksForNothing():
		return listCappedToNothing
	case present == 0:
		return listNothingThere
	case unhidden == 0:
		return listAllHidden
	default:
		return listUnaccounted
	}
}

func emptyListing(remotePath string, reason listEmpty) string {
	unaccounted := fmt.Sprintf("Nothing to show for %s, and no narrowing accounts for it.", remotePath)
	switch reason {
	case listCappedToNothing:
		return "--limit 0 asked for no entries."
	case listNothingThere:
		return fmt.Sprintf("%s is empty.", remotePath)
	case listAllHidden:
		return fmt.Sprintf("%s holds only hidden entries; ifiles list %s -a lists them.", remotePath, remotePath)
	case listUnaccounted:
		return unaccounted
	}
	// A member added to listEmpty and left unnamed above lands here. The answer
	// keeps the reader looking rather than telling them the directory is bare,
	// which is the direction a fallthrough has to fail in.
	return unaccounted
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
	registerLimit(listCmd, &lsLimit, "entries")
	listCmd.Flags().BoolVarP(&lsAll, "all", "a", false, "include hidden entries")
	listCmd.Flags().BoolVarP(&lsLong, "long", "l", false, "show size and modification time")
	rootCmd.AddCommand(listCmd)
}
