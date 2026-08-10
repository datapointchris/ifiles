package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/datapointchris/goselfupdate/cobracmd"
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/auth"
	"github.com/datapointchris/ifiles/v2/config"
	"github.com/datapointchris/ifiles/v2/filebrowser"
)

var (
	configPath  string
	sourceFlag  string
	noInput     bool
	timeoutFlag int
)

var rootCmd = &cobra.Command{
	Use:   "ifiles",
	Short: "Move files in and out of files.ichrisbirch.com from the terminal",
	Long: `ifiles is a client for a FileBrowser Quantum server.

Commands follow the shape: ifiles VERB PATH, and the verb says what it does in
plain words — upload, download, list, read, copy, move, delete. Nothing here
requires remembering which of get and put goes which way.

One rule: the remote path comes first for a download and second for an upload.
"ifiles download REMOTE [LOCAL]" pulls, "ifiles upload LOCAL [REMOTE]" pushes,
so the argument nearest the verb is always the thing being read.

Remote paths are absolute from the source root, and the source, server, and
chunk size are inferred from config rather than typed. Every level is
self-documenting: run any partial command with no arguments or --help to see
what comes next.`,
	Example: `  ifiles list /photos                 what is in a remote directory
  ifiles download /photos/raw.cr2     fetch one file into the current directory
  ifiles upload video.mkv /media      send one, chunked so the tunnel cannot fail it
  ifiles read /notes/todo.md          print a file's contents, for piping
  ifiles search invoice               find a file without opening the browser`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	// os.Exit lives here alone so run() can use defer, which os.Exit skips.
	os.Exit(run())
}

func run() int {
	// Interrupts have to arrive as a context cancellation rather than as a process
	// kill, because an upload's cleanup is a network call: the server is told to
	// keep the partial file, and only then is the transfer aborted. A default
	// SIGINT would skip that and throw away the progress.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	autoConfig := autoupdate.Config{Update: updateConfig()}
	if err := cobracmd.Execute(ctx, rootCmd, autoConfig); err != nil {
		if !errors.Is(err, cobracmd.ErrReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		// 2 says the command line was wrong rather than the run, which is the
		// only failure a caller should retry with different arguments.
		if errors.Is(err, cobracmd.ErrUsage) {
			return 2
		}
		return 1
	}
	return 0
}

// Command groups keep help readable as verbs accumulate, and separating reading
// from acting puts the destructive half in one visible block.
const (
	groupRead = "read"
	groupAct  = "act"
	groupAuth = "auth"
	groupTool = "tool"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupRead, Title: "Reading"},
		&cobra.Group{ID: groupAct, Title: "Acting"},
		&cobra.Group{ID: groupAuth, Title: "Server and access"},
		&cobra.Group{ID: groupTool, Title: "Managing ifiles itself"},
	)

	flags := rootCmd.PersistentFlags()
	flags.StringVarP(&configPath, "config", "c", "", "path to config file (default is $XDG_CONFIG_HOME/ifiles/config.toml)")
	flags.StringVar(&sourceFlag, "source", "", "Quantum source to address (default is the configured source)")
	flags.BoolVar(&noInput, "no-input", false, "never prompt; fail naming the flag that would have answered")
	flags.IntVar(&timeoutFlag, "timeout", 0, "seconds to allow a transfer (0 for no limit)")

	mustCompleteFlag(rootCmd, "source", completeSources)
}

// loadConfig reads the config, honoring --source as an override of the stored
// source name.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if sourceFlag != "" {
		cfg.Source = sourceFlag
	}
	return cfg, nil
}

// newClient builds an authenticated client. The token comes from the token store
// keyed by server URL, so the config file never holds a secret.
func newClient() (*filebrowser.Client, *config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	url, err := cfg.RequireURL()
	if err != nil {
		return nil, nil, err
	}
	token, _, err := auth.NewTokenStore().Load(url)
	if err != nil {
		return nil, nil, err
	}

	client, err := filebrowser.New(filebrowser.Options{
		BaseURL: url,
		Token:   token,
		Source:  cfg.Source,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

// commandContext bounds a command with --timeout when one was given. Transfers
// are otherwise unbounded on purpose: a multi-gigabyte upload legitimately
// outlives any default anyone would pick.
func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	if timeoutFlag <= 0 {
		return cmd.Context(), func() {}
	}
	return context.WithTimeout(cmd.Context(), time.Duration(timeoutFlag)*time.Second)
}
