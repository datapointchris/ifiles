package cmd

import (
	"fmt"
	"path"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

var (
	moveOverride bool
	copyOverride bool
)

var moveCmd = &cobra.Command{
	Use:        "move <src> <dst>",
	GroupID:    groupAct,
	SuggestFor: []string{"rename"},
	Short:      "Move or rename a remote path",
	Long: `Moves a remote path, server-side. Renaming is the same command with a
destination in the same directory.

Nothing is downloaded and re-uploaded — the bytes never leave the server, so moving
a large file is instant regardless of its size. A destination that is an existing
directory means "into it".`,
	Example: `  ifiles move /inbox/scan.pdf /docs/receipts   into a directory
  ifiles move /docs/draft.md /docs/final.md   rename in place`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return moveOrCopy(cmd, moveAction, args[0], args[1], filebrowser.Overwrite(moveOverride))
	},
}

var copyCmd = &cobra.Command{
	Use:     "copy <src> <dst>",
	GroupID: groupAct,
	Short:   "Copy a remote path",
	Long: `Copies a remote path, server-side.

Like move, the data never travels through this client.`,
	Example: `  ifiles copy /docs/report.pdf /archive
  ifiles copy /config/prod.yml /config/staging.yml`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return moveOrCopy(cmd, copyAction, args[0], args[1], filebrowser.Overwrite(copyOverride))
	},
}

// remoteAction is which server-side operation moveOrCopy runs.
type remoteAction int

const (
	moveAction remoteAction = iota
	copyAction
)

func moveOrCopy(cmd *cobra.Command, action remoteAction, src, dst string, overwrite filebrowser.Overwrite) error {
	client, _, err := newClient()
	if err != nil {
		return err
	}

	from := filebrowser.CleanPath(src)
	to := filebrowser.CleanPath(dst)

	ctx, cancel := commandContext(cmd)
	defer cancel()

	if _, err := client.Stat(ctx, from); err != nil {
		return err
	}
	if info, err := client.Stat(ctx, to); err == nil && info.IsDir() {
		to = path.Join(to, path.Base(from))
	}

	if action == moveAction {
		err = client.Move(ctx, from, to, overwrite)
	} else {
		err = client.Copy(ctx, from, to, overwrite)
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

func pastTense(action remoteAction) string {
	if action == moveAction {
		return "Moved"
	}
	return "Copied"
}

func init() {
	moveCmd.Flags().BoolVar(&moveOverride, "override", false, "replace the destination if it exists")
	copyCmd.Flags().BoolVar(&copyOverride, "override", false, "replace the destination if it exists")
}
