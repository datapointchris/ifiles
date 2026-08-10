package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/config"
	"github.com/datapointchris/ifiles/v2/filebrowser"
	"github.com/datapointchris/ifiles/v2/resume"
)

var (
	putOverride  bool
	putNoResume  bool
	putChunkSize string
)

var uploadCmd = &cobra.Command{
	Use:        "upload <local> [remote]",
	GroupID:    groupAct,
	SuggestFor: []string{"put", "push", "send"},
	Short:      "Upload a file or directory",
	Long: `Uploads a local path, recursively for a directory.

Every upload is chunked, not only large ones. The tunnel in front of this server
caps a request body at 100 MB, so an unchunked upload of a large file is rejected
outright and no retry clears it — and WebDAV, which rclone would use, has no
chunking at all. This command is the only way a large file gets through.

Interrupting is safe. On Ctrl-C the server is told to keep what it has before the
transfer is aborted, the offset is recorded locally, and re-running the same
command continues from there. The offset has to be remembered on this side because
the server does not report it; a local file that changed between attempts
invalidates the resume, since the server would otherwise write new bytes into the
middle of an old upload.`,
	Example: `  ifiles upload video.mkv                 into the configured remote directory
  ifiles upload video.mkv /media          into a directory
  ifiles upload video.mkv /media/new.mkv  under a different name
  ifiles upload ~/photos /backup          a whole tree, recursively
  ifiles upload big.iso --chunk-size 8MiB smaller chunks over a flaky link`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, cfg, err := newClient()
		if err != nil {
			return err
		}

		localPath := args[0]
		info, err := os.Stat(localPath)
		if err != nil {
			return err
		}

		if putChunkSize != "" {
			cfg.ChunkSize = putChunkSize
		}
		chunkSize, err := cfg.Chunks()
		if err != nil {
			return err
		}

		remoteBase := cfg.RemoteDir
		if remoteBase == "" {
			remoteBase = "/"
		}
		if len(args) == 2 {
			remoteBase = args[1]
		}

		if info.IsDir() {
			return putDirectory(cmd, client, localPath, remoteBase, chunkSize)
		}

		remotePath := resolveRemoteFile(cmd, client, remoteBase, filepath.Base(localPath))
		return putFile(cmd, client, localPath, remotePath, chunkSize)
	},
}

// resolveRemoteFile decides whether the remote argument names a directory to
// upload into or the destination filename itself, by asking the server. A path
// that does not exist yet is taken as the filename, which is what makes
// `upload file.txt /dir/renamed.txt` work without a flag.
func resolveRemoteFile(cmd *cobra.Command, client *filebrowser.Client, remote, localName string) string {
	remote = filebrowser.CleanPath(remote)

	ctx, cancel := commandContext(cmd)
	defer cancel()

	if info, err := client.Stat(ctx, remote); err == nil && info.IsDir() {
		return path.Join(remote, localName)
	}
	if remote == "/" {
		return path.Join("/", localName)
	}
	return remote
}

func putFile(cmd *cobra.Command, client *filebrowser.Client, localPath, remotePath string, chunkSize int64) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	store := resume.NewStore()
	// The fingerprint is the local file: if it changed, the bytes already on the
	// server belong to a different version and resuming would splice the two.
	record := resume.Record{
		Kind:            resume.Upload,
		Source:          client.Source(),
		RemotePath:      remotePath,
		TotalSize:       info.Size(),
		FingerprintSize: info.Size(),
		FingerprintMod:  info.ModTime(),
	}

	offset := int64(0)
	if !putNoResume {
		// -1: how much of a partial upload the server holds cannot be observed from
		// here, so the recorded offset is taken on trust after the fingerprint check.
		if recorded, ok := store.Lookup(record, -1); ok {
			offset = recorded
			infof(cmd, "Resuming %s at %s of %s.", remotePath, config.FormatSize(offset), config.FormatSize(info.Size()))
		}
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	bar := newProgress(cmd.ErrOrStderr(), filepath.Base(localPath), info.Size(), showProgress(cmd))
	bar.Set(offset)

	ctx, cancel := commandContext(cmd)
	defer cancel()

	uploadErr := client.Upload(ctx, file, filebrowser.UploadRequest{
		Path:      remotePath,
		Size:      info.Size(),
		ChunkSize: chunkSize,
		Offset:    offset,
		// A resumed upload always overrides: the first chunk of the resumed range
		// is not offset zero, but the server still checks for a conflicting
		// destination, and the destination is the file we were already writing.
		Override: putOverride || offset > 0,
		Progress: func(sent int64) {
			bar.Set(sent)
			// Recorded per chunk rather than at the end, because the point of the
			// record is to survive the case where there is no end.
			progressed := record
			progressed.Offset = sent
			_ = store.Save(progressed)
		},
	})
	bar.Done()

	if uploadErr != nil {
		if errors.Is(uploadErr, filebrowser.ErrUploadPaused) {
			infof(cmd, "Paused. Re-run the same command to resume.")
			return uploadErr
		}
		if filebrowser.IsConflict(uploadErr) {
			return fmt.Errorf("%s already exists; pass --override to replace it", remotePath)
		}
		return uploadErr
	}

	if err := store.Clear(record); err != nil {
		return err
	}
	infof(cmd, "Uploaded %s (%s).", remotePath, config.FormatSize(info.Size()))
	return nil
}

// putDirectory walks a local tree, creating each directory before uploading the
// files inside it. Directories are created explicitly rather than relying on the
// upload to make parents, so an empty directory still arrives.
func putDirectory(cmd *cobra.Command, client *filebrowser.Client, localRoot, remoteBase string, chunkSize int64) error {
	remoteRoot := path.Join(filebrowser.CleanPath(remoteBase), filepath.Base(filepath.Clean(localRoot)))

	return filepath.WalkDir(localRoot, func(localPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}

		relative, err := filepath.Rel(localRoot, localPath)
		if err != nil {
			return err
		}
		remotePath := remoteRoot
		if relative != "." {
			remotePath = path.Join(remoteRoot, filepath.ToSlash(relative))
		}

		if entry.IsDir() {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			if err := client.Mkdir(ctx, remotePath); err != nil && !filebrowser.IsConflict(err) {
				return err
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			infof(cmd, "Skipping %s: not a regular file.", localPath)
			return nil
		}
		return putFile(cmd, client, localPath, remotePath, chunkSize)
	})
}

func init() {
	uploadCmd.Flags().BoolVar(&putOverride, "override", false, "replace an existing remote file")
	uploadCmd.Flags().BoolVar(&putNoResume, "no-resume", false, "ignore a recorded offset and upload from the start")
	uploadCmd.Flags().StringVar(&putChunkSize, "chunk-size", "", "chunk size for this upload, e.g. 8MiB (default is 32MiB)")
	// Suggestions, not the accepted set: any size ParseSize understands is valid,
	// and these are the ones worth reaching for on a link that keeps dropping.
	mustCompleteFlag(uploadCmd, "chunk-size", cobra.FixedCompletions(
		[]string{"4MiB", "8MiB", "16MiB", "32MiB", "64MiB"},
		cobra.ShellCompDirectiveNoFileComp,
	))
	rootCmd.AddCommand(uploadCmd)
}
