package cmd

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/datapointchris/ifiles/config"
	"github.com/datapointchris/ifiles/filebrowser"
	"github.com/datapointchris/ifiles/resume"
)

var (
	getArchive  string
	getOverride bool
	getNoResume bool
)

// partialSuffix marks an incomplete download. Writing to a suffixed file means a
// half-finished transfer can never be mistaken for the real thing by whatever
// reads the directory next, and it is also the resume marker: its size is the
// offset to continue from.
const partialSuffix = ".ifilespart"

var getCmd = &cobra.Command{
	Use:     "get <remote> [local]",
	GroupID: groupRead,
	Short:   "Download a file or directory",
	Long: `Downloads a remote path.

A file arrives as itself. A directory arrives as an archive, built and streamed by
the server — there is no separate step to trigger, and nothing is staged locally
first.

A single-file download resumes: bytes land in a ".ifilespart" file that is renamed
into place at the end, and re-running asks the server only for the bytes past what
is already there. An archive cannot resume, because the server generates it as it
streams and has nothing to seek to.`,
	Example: `  ifiles get /photos/raw.cr2              into the current directory
  ifiles get /photos/raw.cr2 ~/Pictures  into a directory
  ifiles get /photos/raw.cr2 shot.cr2    under a different name
  ifiles get /photos --archive tar.gz    a whole directory as a tarball
  ifiles get /notes/todo.md -            to stdout, for piping`,
	Args: cobra.RangeArgs(1, 2),
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

		archive := getArchive
		if info.IsDir() && archive == "" {
			archive = filebrowser.ArchiveZip
		}
		if !info.IsDir() && archive != "" {
			return fmt.Errorf("%s is a file; --archive only applies to a directory", remotePath)
		}

		toStdout := len(args) == 2 && args[1] == "-"
		if toStdout {
			return getToStdout(cmd, client, remotePath, archive)
		}

		target, err := localTarget(remotePath, archive, args)
		if err != nil {
			return err
		}
		return getToFile(cmd, client, remotePath, target, archive, info)
	},
}

// localTarget works out where the bytes go. A destination that is an existing
// directory means "inside it", matching cp and scp; anything else is the filename
// itself.
func localTarget(remotePath, archive string, args []string) (string, error) {
	name := path.Base(remotePath)
	if archive != "" {
		name += "." + archive
	}

	if len(args) < 2 {
		return name, nil
	}

	target := args[1]
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return filepath.Join(target, name), nil
	}
	if strings.HasSuffix(target, string(os.PathSeparator)) {
		return "", fmt.Errorf("directory %s does not exist", target)
	}
	return target, nil
}

func getToStdout(cmd *cobra.Command, client *filebrowser.Client, remotePath, archive string) error {
	ctx, cancel := commandContext(cmd)
	defer cancel()

	download, err := client.Download(ctx, filebrowser.DownloadRequest{
		Paths:   []string{remotePath},
		Archive: archive,
	})
	if err != nil {
		return err
	}
	defer func() { _ = download.Close() }()

	_, err = io.Copy(cmd.OutOrStdout(), download.Body)
	return err
}

func getToFile(cmd *cobra.Command, client *filebrowser.Client, remotePath, target, archive string, remote *filebrowser.Listing) error {
	if _, err := os.Stat(target); err == nil && !getOverride {
		return fmt.Errorf("%s already exists; pass --override to replace it", target)
	}

	remoteSize := remote.Size
	partial := target + partialSuffix
	store := resume.NewStore()
	// The fingerprint is the remote object. A partial file's length is not proof
	// its contents came from this version of this path — a changed remote file, or
	// a leftover partial from a different download to the same name, would
	// otherwise have the new tail appended to the old head. That produces a file of
	// exactly the right size and entirely wrong contents, with no error anywhere.
	record := resume.Record{
		Kind:            resume.Download,
		Source:          client.Source(),
		RemotePath:      remotePath,
		LocalPath:       target,
		TotalSize:       remoteSize,
		FingerprintSize: remoteSize,
		FingerprintMod:  remote.Modified,
	}

	offset := int64(0)
	// An archive is generated on the fly and cannot be ranged into, so a stale
	// partial from a previous attempt has to be discarded rather than continued.
	if archive == "" && !getNoResume {
		if info, err := os.Stat(partial); err == nil {
			if recorded, ok := store.Lookup(record, info.Size()); ok {
				offset = recorded
			}
		}
	}

	ctx, cancel := commandContext(cmd)
	defer cancel()

	download, err := client.Download(ctx, filebrowser.DownloadRequest{
		Paths:   []string{remotePath},
		Archive: archive,
		Offset:  offset,
	})
	if err != nil {
		return err
	}
	defer func() { _ = download.Close() }()

	// The server may ignore the range request, in which case the body starts from
	// byte zero and appending would duplicate everything already on disk.
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && download.Resumed {
		flags |= os.O_APPEND
		infof(cmd, "Resuming %s at %s.", target, config.FormatSize(offset))
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}

	file, err := os.OpenFile(partial, flags, 0o644)
	if err != nil {
		return err
	}

	total := remoteSize
	if archive != "" {
		// An archive's finished size is not known until it has been built.
		total = 0
	}
	bar := newProgress(cmd.ErrOrStderr(), path.Base(remotePath), total, showProgress(cmd))
	bar.Set(offset)
	// Recorded as it goes, so an interrupt leaves behind a partial whose provenance
	// is known. Written on the same throttle as the progress line rather than per
	// read, because this is a file write per call.
	bar.onTick = func(done int64) {
		if archive == "" {
			progressed := record
			progressed.Offset = done
			_ = store.Save(progressed)
		}
	}

	written, copyErr := io.Copy(file, &countingReader{reader: download.Body, progress: bar})
	bar.Done()

	if err := file.Close(); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		// The partial file is kept deliberately: it is the resume point, and
		// deleting it would turn one interrupted transfer into a full re-download.
		if archive == "" {
			if info, statErr := os.Stat(partial); statErr == nil {
				final := record
				final.Offset = info.Size()
				_ = store.Save(final)
			}
		}
		return fmt.Errorf("%w (partial download kept at %s)", copyErr, partial)
	}

	if err := os.Rename(partial, target); err != nil {
		return err
	}
	if err := store.Clear(record); err != nil {
		return err
	}
	infof(cmd, "Wrote %s (%s).", target, config.FormatSize(offset+written))
	return nil
}

// showProgress reports whether a progress line would be useful: it is noise in a
// log and meaningless when stderr is not a terminal.
func showProgress(cmd *cobra.Command) bool {
	file, ok := cmd.ErrOrStderr().(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func init() {
	getCmd.Flags().StringVar(&getArchive, "archive", "", "archive format for a directory: zip or tar.gz")
	mustCompleteFlag(getCmd, "archive", cobra.FixedCompletions(
		[]string{filebrowser.ArchiveZip, filebrowser.ArchiveTarGz},
		cobra.ShellCompDirectiveNoFileComp,
	))
	getCmd.Flags().BoolVar(&getOverride, "override", false, "replace an existing local file")
	getCmd.Flags().BoolVar(&getNoResume, "no-resume", false, "ignore a partial download and start over")
	rootCmd.AddCommand(getCmd)
}
