package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

var mkdirCmd = &cobra.Command{
	Use:     "mkdir <path>",
	GroupID: groupAct,
	Short:   "Create a remote directory",
	Long: `Creates a directory.

Intermediate directories are created too — the server does that itself, so there is
no -p to pass.`,
	Example: `  ifiles mkdir /photos/2026
  ifiles mkdir /projects/new/assets/raw   parents included`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		remotePath := filebrowser.CleanPath(args[0])
		ctx, cancel := commandContext(cmd)
		defer cancel()

		if err := client.Mkdir(ctx, remotePath); err != nil {
			if filebrowser.IsConflict(err) {
				return fmt.Errorf("%s already exists", remotePath)
			}
			return err
		}

		infof(cmd, "Created %s.", remotePath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mkdirCmd)
}
