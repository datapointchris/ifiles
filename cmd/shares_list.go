package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

// shareTimeFormat is a minute-resolution local timestamp. A share's lifetime is
// set in hours and days, so seconds are noise and a full RFC 3339 stamp costs a
// column the path could have used.
const shareTimeFormat = "2006-01-02 15:04"

var (
	shareListJSON  bool
	shareListLimit int
)

var sharesListCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List the share links this account can see",
	Long: `Lists share links, newest information the server has on each.

With no argument this is every link the account can see — an admin account sees
everyone's, because the server branches on the user record rather than on a
parameter. With a path it is the links pointing at that path.

The URL is the last column because it is what you came for, and because it is
also the handle: it pastes straight back into "ifiles shares delete". An expired link
is still listed, since the server keeps it in storage, and is marked as expired
rather than hidden. A link whose file has since been deleted is marked "gone" —
it still resolves, and 404s for whoever was sent it.`,
	Example: `  ifiles shares list
  ifiles shares list /photos
  ifiles shares list --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		var shares []filebrowser.Share
		if len(args) == 1 {
			shares, err = client.SharesForPath(ctx, filebrowser.CleanPath(args[0]))
		} else {
			shares, err = client.Shares(ctx)
		}
		if err != nil {
			return shareError(err)
		}
		if shareListLimit > 0 && len(shares) > shareListLimit {
			shares = shares[:shareListLimit]
		}

		if shareListJSON {
			return emitJSON(cmd, shares)
		}

		if len(shares) == 0 {
			infof(cmd, "No share links.")
			return nil
		}

		table := newTable(cmd.OutOrStdout())
		writef(table, "PATH\tEXPIRES\tDOWNLOADS\tPASSWORD\tURL\n")
		for _, share := range shares {
			writef(table, "%s\t%s\t%s\t%t\t%s\n",
				share.Path,
				shareExpiryColumn(share),
				shareDownloadsColumn(share),
				share.HasPassword,
				share.ShareURL)
		}
		return table.Flush()
	},
}

// shareExpiryColumn folds three states into one column, because a link that has
// lapsed and a link that outlived its file are both dead and neither is
// distinguishable from a date alone.
func shareExpiryColumn(share filebrowser.Share) string {
	switch {
	case !share.PathExists:
		return "gone"
	case share.Expired():
		return "expired"
	case share.ExpiresAt().IsZero():
		return "never"
	default:
		return share.ExpiresAt().Local().Format(shareTimeFormat)
	}
}

func shareDownloadsColumn(share filebrowser.Share) string {
	if share.DownloadsLimit > 0 {
		return fmt.Sprintf("%d/%d", share.Downloads, share.DownloadsLimit)
	}
	return strconv.Itoa(share.Downloads)
}

func init() {
	sharesListCmd.Flags().BoolVar(&shareListJSON, "json", false, "Output shares as JSON to stdout")
	sharesListCmd.Flags().IntVarP(&shareListLimit, "limit", "n", 0, "maximum shares to show (0 for all)")
	sharesCmd.AddCommand(sharesListCmd)
}
