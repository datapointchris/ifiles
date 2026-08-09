package cmd

import (
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

var sharesDeleteCmd = &cobra.Command{
	Use:        "delete <hash-or-url>",
	SuggestFor: []string{"rm", "revoke"},
	Short:      "Revoke a share link",
	Long: `Revokes a share link so the URL stops working.

Takes what a listing prints: the share URL pastes in whole, and so does a bare
hash or a download URL. Nothing on the server is deleted — the file the link
pointed at is untouched, and only the link dies.

There is no confirmation. A revoked link is recreated with one command, so the
cost of getting this wrong is a new URL rather than lost data, which is the
opposite of "ifiles delete".`,
	Example: `  ifiles shares delete T7bQ3xkLm2
  ifiles shares delete https://files.ichrisbirch.com/public/share/T7bQ3xkLm2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		hash, err := shareHash(args[0])
		if err != nil {
			return err
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		if err := client.DeleteShare(ctx, hash); err != nil {
			// The delete handler answers 400, not 404, for a hash it cannot find —
			// it fails while looking the share up, before it decides anything is
			// missing — so IsNotFound would never fire here.
			if filebrowser.IsBadRequest(err) {
				return &shareNotFoundError{Hash: hash}
			}
			return shareError(err)
		}

		infof(cmd, "Revoked share %s.", hash)
		return nil
	},
}

type shareNotFoundError struct {
	Hash string
}

func (e *shareNotFoundError) Error() string {
	return "no share with hash " + e.Hash + "; `ifiles shares list` shows the live ones"
}

func init() {
	sharesCmd.AddCommand(sharesDeleteCmd)
}
