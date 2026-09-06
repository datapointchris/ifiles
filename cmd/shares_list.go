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
	shareListLimit limitFlag
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
		scope := ""
		if len(args) == 1 {
			scope = filebrowser.CleanPath(args[0])
			shares, err = client.SharesForPath(ctx, scope)
		} else {
			shares, err = client.Shares(ctx)
		}
		if err != nil {
			return shareError(err)
		}
		found := len(shares)
		shares = limited(shares, shareListLimit)

		// Above the --json return, so a caller reading the machine door is told
		// why the set is empty or short rather than being handed two bytes that
		// could mean either. Both go to stderr, and shortened is deferred so it
		// lands under what it describes.
		if len(shares) == 0 {
			reason := sharesEmptiness(shareListLimit, scope, found)
			infof(cmd, "%s", emptyShares(scope, reason))
		}
		defer shortened(cmd, len(shares), found, shareListLimit)

		if shareListJSON {
			return emitJSON(cmd, shares)
		}
		if len(shares) == 0 {
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

// sharesEmpty names what left a listing of links with no rows. Its members are
// this command's own, so a reason belonging to another verb cannot reach the
// renderer below.
type sharesEmpty int

const (
	// sharesUnaccounted is the zero value, so a narrowing added later and left
	// unclassified answers with the sentence that keeps the reader looking.
	sharesUnaccounted sharesEmpty = iota
	sharesNoneOnTheAccount
	sharesNoneForThePath
	sharesCappedToNothing
)

// sharesEmptyReasons is every member, for the test that walks them.
var sharesEmptyReasons = []sharesEmpty{
	sharesUnaccounted, sharesNoneOnTheAccount, sharesNoneForThePath, sharesCappedToNothing,
}

// sharesEmptiness takes the cap rather than inferring it from the count, and
// asks it first because a cap of zero voids the widening the path answer names.
func sharesEmptiness(limit limitFlag, scope string, found int) sharesEmpty {
	switch {
	case limit.asksForNothing():
		return sharesCappedToNothing
	case scope != "":
		return sharesNoneForThePath
	case found == 0:
		return sharesNoneOnTheAccount
	default:
		return sharesUnaccounted
	}
}

func emptyShares(scope string, reason sharesEmpty) string {
	const unaccounted = "Nothing to show, and no narrowing accounts for it."
	switch reason {
	case sharesCappedToNothing:
		return "--limit 0 asked for no share links."
	case sharesNoneOnTheAccount:
		return "No share links."
	case sharesNoneForThePath:
		return fmt.Sprintf("No share links for %s; ifiles shares list lists every link.", scope)
	case sharesUnaccounted:
		return unaccounted
	}
	// A member added to sharesEmpty and left unnamed above lands here, on the
	// answer that keeps the reader looking rather than the one that closes.
	return unaccounted
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
	registerLimit(sharesListCmd, &shareListLimit, "share links")
	sharesCmd.AddCommand(sharesListCmd)
}
