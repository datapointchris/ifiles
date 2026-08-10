package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/datapointchris/ifiles/v3/filebrowser"
)

var rmForce bool

var deleteCmd = &cobra.Command{
	Use:        "delete <path>",
	GroupID:    groupAct,
	SuggestFor: []string{"rm", "remove", "del"},
	Short:      "Delete a remote file or directory",
	Long: `Deletes a remote path.

A directory takes its contents with it, so the confirmation names how many entries
are about to go. On a terminal it asks; without one it refuses unless --force is
given, rather than prompting into a stdin that will never answer.`,
	Example: `  ifiles delete /photos/blurry.jpg
  ifiles delete /tmp/scratch --force   no confirmation, for a script`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := newClient()
		if err != nil {
			return err
		}

		remotePath := filebrowser.CleanPath(args[0])
		if remotePath == "/" {
			return fmt.Errorf("refusing to delete the source root")
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		info, err := client.Stat(ctx, remotePath)
		if err != nil {
			if filebrowser.IsNotFound(err) {
				return fmt.Errorf("%s does not exist on the server", remotePath)
			}
			return err
		}

		if err := confirmDelete(cmd, remotePath, info); err != nil {
			return err
		}
		if err := client.Delete(ctx, remotePath); err != nil {
			return err
		}

		infof(cmd, "Deleted %s.", remotePath)
		return nil
	},
}

// confirmDelete scales the friction to the blast radius: a file is one y/N, a
// directory says what goes with it first.
func confirmDelete(cmd *cobra.Command, remotePath string, info *filebrowser.Listing) error {
	if rmForce {
		return nil
	}

	stdin := int(os.Stdin.Fd())
	if noInput || !term.IsTerminal(stdin) {
		return fmt.Errorf("refusing to delete %s without confirmation; pass --force", remotePath)
	}

	what := remotePath
	if info.IsDir() {
		what = fmt.Sprintf("%s and its %d entries", remotePath, len(info.Files)+len(info.Folders))
	}
	writef(cmd.ErrOrStderr(), "Delete %s? [y/N] ", what)

	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return fmt.Errorf("canceled")
	}
	return nil
}

func init() {
	deleteCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "delete without confirmation")
	rootCmd.AddCommand(deleteCmd)
}
