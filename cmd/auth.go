package cmd

import "github.com/spf13/cobra"

var authCmd = &cobra.Command{
	Use:     "auth",
	GroupID: groupAuth,
	Short:   "Store and check the API token ifiles authenticates with",
	Long: `Authentication commands.

The server is OIDC-only, so there is no password to send and no headless login
flow. Access is a long-lived API token instead, and creating the first one
requires a browser session — so the token is minted in the web UI under
Settings, then pasted in here once. It is stored in the OS keyring, keyed by
server URL, and never written to the config file.`,
	Example: `  ifiles auth login              paste a token minted in the web UI
  ifiles auth status             is the server up and the token still valid
  ifiles auth logout             forget the token for this server`,
	// Help is never wrong; a bare `ifiles auth` teaches the verbs rather than
	// erroring on a missing subcommand.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
}
