package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/auth"
)

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Delete the stored token for a server",
	Long: `Removes the stored token, from the OS keyring and the fallback file both.

The token stays valid on the server — this only forgets it locally. To stop it
working, revoke it with "ifiles tokens rm" or in the web UI.`,
	Example: `  ifiles auth logout`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		url, err := cfg.RequireURL()
		if err != nil {
			return err
		}

		if err := auth.NewTokenStore().Delete(url); err != nil {
			if errors.Is(err, auth.ErrNotLoggedIn) {
				infof(cmd, "No token stored for %s.", url)
				return nil
			}
			return err
		}

		infof(cmd, "Deleted the token for %s. It is still valid on the server; revoke it to stop it working.", url)
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLogoutCmd)
}
