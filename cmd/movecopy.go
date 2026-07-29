package cmd

import (
	"fmt"
	"path"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

var (
	mvOverride bool
	cpOverride bool
)

var mvCmd = &cobra.Command{
	Use:     "mv <src> <dst>",
	GroupID: groupAct,
	Short:   "Move or rename a remote path",
	Long: `Moves a remote path, server-side.

Nothing is downloaded and re-uploaded — the bytes never leave the server, so moving
a large file is instant regardless of its size. A destination that is an existing
directory means "into it", matching mv.`,
	Example: `  ifiles mv /inbox/scan.pdf /docs/receipts   into a directory
  ifiles mv /docs/draft.md /docs/final.md   rename in place`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return moveOrCopy(cmd, "move", args[0], args[1], mvOverride)
	},
}

var cpCmd = &cobra.Command{
	Use:     "cp <src> <dst>",
	GroupID: groupAct,
	Short:   "Copy a remote path",
	Long: `Copies a remote path, server-side.

Like mv, the data never travels through this client.`,
	Example: `  ifiles cp /docs/report.pdf /archive
  ifiles cp /config/prod.yml /config/staging.yml`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return moveOrCopy(cmd, "copy", args[0], args[1], cpOverride)
	},
}

func moveOrCopy(cmd *cobra.Command, action, src, dst string, override bool) error {
	client, _, err := newClient()
	if err != nil {
		return err
	}

	from := filebrowser.CleanPath(src)
	to := filebrowser.CleanPath(dst)

	ctx, cancel := commandContext(cmd)
	defer cancel()

	if _, err := client.Stat(ctx, from); err != nil {
		if filebrowser.IsNotFound(err) {
			return fmt.Errorf("%s does not exist on the server", from)
		}
		return err
	}
	if info, err := client.Stat(ctx, to); err == nil && info.IsDir() {
		to = path.Join(to, path.Base(from))
	}

	if action == "move" {
		err = client.Move(ctx, from, to, override)
	} else {
		err = client.Copy(ctx, from, to, override)
	}
	if err != nil {
		if filebrowser.IsConflict(err) {
			return fmt.Errorf("%s already exists; pass --override to replace it", to)
		}
		return err
	}

	infof(cmd, "%s %s to %s.", pastTense(action), from, to)
	return nil
}

func pastTense(action string) string {
	if action == "move" {
		return "Moved"
	}
	return "Copied"
}

func init() {
	mvCmd.Flags().BoolVar(&mvOverride, "override", false, "replace the destination if it exists")
	cpCmd.Flags().BoolVar(&cpOverride, "override", false, "replace the destination if it exists")
	rootCmd.AddCommand(mvCmd, cpCmd)
}
