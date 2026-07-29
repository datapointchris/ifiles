package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

var catCmd = &cobra.Command{
	Use:     "cat <path>",
	GroupID: groupRead,
	Short:   "Stream a remote file to stdout",
	Long: `Writes a remote file to stdout, for piping.

Nothing else is written to stdout, so the output is exactly the file — progress and
diagnostics go to stderr, which is what makes this safe on the left of a pipe.`,
	Example: `  ifiles cat /notes/todo.md
  ifiles cat /logs/app.log | rg ERROR
  ifiles cat /data/export.csv > local.csv`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		remotePath := filebrowser.CleanPath(args[0])
		ctx, cancel := commandContext(cmd)
		defer cancel()

		info, err := client.Stat(ctx, remotePath)
		if err != nil {
			if filebrowser.IsNotFound(err) {
				return fmt.Errorf("%s does not exist on the server", remotePath)
			}
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory; use `ifiles get %s` to download it", remotePath, remotePath)
		}

		download, err := client.Download(ctx, filebrowser.DownloadRequest{Paths: []string{remotePath}})
		if err != nil {
			return err
		}
		defer func() { _ = download.Close() }()

		_, err = io.Copy(cmd.OutOrStdout(), download.Body)
		return err
	},
}

func init() {
	rootCmd.AddCommand(catCmd)
}
