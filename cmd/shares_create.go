package cmd

import (
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/config"
	"github.com/datapointchris/ifiles/filebrowser"
)

var (
	shareExpires    string
	sharePassword   bool
	shareDownloads  int
	shareCreateJSON bool
)

var sharesCreateCmd = &cobra.Command{
	Use:   "create <path>",
	Short: "Publish a link to a remote file or directory",
	Long: `Creates a public link to a remote path and prints its URL.

The URL is the only thing on stdout, so it pipes into a clipboard or a message
without anything to strip. Everything else the command has to say goes to stderr.

--expires takes a window rather than a date: 7d, 12h, 90m. Without it the link
never expires. --password reads the password from a hidden prompt, or from stdin
when there is no terminal, so it is never in shell history or in ps output.

A directory shares the whole subtree, and the server builds the archive itself
when the recipient downloads it.`,
	Example: `  ifiles shares create /photos/wedding
  ifiles shares create /docs/contract.pdf --expires 7d --password
  ifiles shares create /media/talk.mp4 --expires 24h --downloads 1`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		request := filebrowser.ShareRequest{
			Path:           filebrowser.CleanPath(args[0]),
			DownloadsLimit: shareDownloads,
		}
		if shareExpires != "" {
			expires, err := config.ParseDuration(shareExpires)
			if err != nil {
				return err
			}
			request.Expires = expires
		}
		if sharePassword {
			password, err := readSecret(cmd, secretPrompt{
				Label:   "Password for the link: ",
				Missing: "no password given",
				NoInput: "--no-input given; pass the password on stdin",
			}, false)
			if err != nil {
				return err
			}
			request.Password = password
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		share, err := client.CreateShare(ctx, request)
		if err != nil {
			return shareError(err)
		}

		if shareCreateJSON {
			return emitJSON(cmd, share)
		}

		outf(cmd, "%s", share.ShareURL)
		infof(cmd, "Shares %s.", request.Path)
		if expiry := share.ExpiresAt(); expiry.IsZero() {
			infof(cmd, "Never expires; revoke it with `ifiles shares delete %s`.", share.Hash)
		} else {
			infof(cmd, "Expires %s.", expiry.Local().Format(shareTimeFormat))
		}
		if share.HasPassword {
			infof(cmd, "A password is required to open it.")
		}
		if share.DownloadsLimit > 0 {
			infof(cmd, "Stops working after %d downloads.", share.DownloadsLimit)
		}
		return nil
	},
}

func init() {
	sharesCreateCmd.Flags().StringVar(&shareExpires, "expires", "", "how long the link lives, e.g. 7d (default is never)")
	sharesCreateCmd.Flags().BoolVar(&sharePassword, "password", false, "require a password, read from a hidden prompt")
	sharesCreateCmd.Flags().IntVar(&shareDownloads, "downloads", 0, "stop the link working after this many downloads (0 for no limit)")
	sharesCreateCmd.Flags().BoolVar(&shareCreateJSON, "json", false, "Output the created share as JSON to stdout")
	// Suggestions, not the accepted set: ParseDuration takes any of these units,
	// and these are the windows a link is actually set to.
	mustCompleteFlag(sharesCreateCmd, "expires", cobra.FixedCompletions(
		[]string{"1h", "12h", "24h", "7d", "30d"},
		cobra.ShellCompDirectiveNoFileComp,
	))
	sharesCmd.AddCommand(sharesCreateCmd)
}
