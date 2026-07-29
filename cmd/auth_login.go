package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/datapointchris/ifiles/auth"
	"github.com/datapointchris/ifiles/config"
	"github.com/datapointchris/ifiles/filebrowser"
)

var authLoginCmd = &cobra.Command{
	Use:   "login [server-url]",
	Short: "Store an API token for a server",
	Long: `Stores an API token in the OS keyring and records the server URL.

Mint the token in the web UI first: Settings, then API tokens. The account needs
the "api" permission for that page to work — without it the server answers 403,
because creating a token is itself an API-permissioned action.

The token is read from a hidden prompt, or from stdin when given "-", so it never
appears in shell history or in ps output. Login then calls the server to verify
it, and records the source name it finds, which every later command needs and
nobody should have to type.`,
	Example: `  ifiles auth login https://files.ichrisbirch.com
  ifiles auth login                       re-authenticate the configured server
  pass show ifiles | ifiles auth login -  read the token from stdin`,
	// Two positionals, because the server URL and the "-" that redirects the token
	// to stdin are independent: a first login from a script needs both.
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fromStdin := false
		urls := 0
		for _, arg := range args {
			if arg == "-" {
				fromStdin = true
				continue
			}
			urls++
			cfg.URL = strings.TrimRight(arg, "/")
		}
		if urls > 1 {
			return fmt.Errorf("give at most one server URL")
		}

		url, err := cfg.RequireURL()
		if err != nil {
			return err
		}

		token, err := readToken(cmd, fromStdin)
		if err != nil {
			return err
		}

		client, err := filebrowser.New(filebrowser.Options{BaseURL: url, Token: token, Source: cfg.Source})
		if err != nil {
			return err
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		// Verify and discover in one call: /settings/sources is the only route that
		// both proves the token works and reveals a source name.
		sources, err := client.Sources(ctx)
		if err != nil {
			if filebrowser.IsUnauthorized(err) {
				return fmt.Errorf("%s rejected the token; mint a new one in the web UI under Settings", url)
			}
			return err
		}
		if len(sources) == 0 {
			return fmt.Errorf("%s reports no sources for this account", url)
		}

		if err := chooseSource(cmd, cfg, sources); err != nil {
			return err
		}
		if err := auth.NewTokenStore().Save(url, token); err != nil {
			return fmt.Errorf("saving token to keyring: %w", err)
		}
		if err := cfg.Save(); err != nil {
			return err
		}

		infof(cmd, "Logged in to %s as source %q.", url, cfg.Source)
		infof(cmd, "Config: %s", cfg.Path())
		return nil
	},
}

// chooseSource records which source later commands address. With one source the
// answer is not a question; with several, guessing would silently point every
// transfer at the wrong tree, so it asks for --source instead.
func chooseSource(cmd *cobra.Command, cfg *config.Config, sources []filebrowser.Source) error {
	if cfg.Source != "" {
		for _, source := range sources {
			if source.Name == cfg.Source {
				return nil
			}
		}
		names := make([]string, 0, len(sources))
		for _, source := range sources {
			names = append(names, source.Name)
		}
		return fmt.Errorf("source %q does not exist on the server; available: %s", cfg.Source, strings.Join(names, ", "))
	}

	if len(sources) == 1 {
		cfg.Source = sources[0].Name
		return nil
	}

	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}
	return fmt.Errorf("server has %d sources; re-run with --source to pick one: %s", len(sources), strings.Join(names, ", "))
}

// readToken takes the token from stdin or a hidden prompt. A prompt is only
// offered on a TTY: prompting a non-interactive caller would block on a stdin
// that never closes, leaving it with no output and no exit code.
func readToken(cmd *cobra.Command, fromStdin bool) (string, error) {
	stdin := int(os.Stdin.Fd())
	interactive := !fromStdin && !noInput && term.IsTerminal(stdin)

	if !interactive {
		if noInput && !fromStdin {
			return "", fmt.Errorf("--no-input given; pass the token on stdin with `ifiles auth login -`")
		}
		data, err := readAllStdin()
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(data)
		if token == "" {
			return "", fmt.Errorf("no token on stdin")
		}
		return token, nil
	}

	infof(cmd, "Paste the API token from the web UI (Settings, API tokens).")
	writef(cmd.ErrOrStderr(), "Token: ")
	raw, err := term.ReadPassword(stdin)
	writef(cmd.ErrOrStderr(), "\n")
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("no token entered")
	}
	return token, nil
}

func init() {
	authLoginCmd.Flags().SortFlags = false
	authCmd.AddCommand(authLoginCmd)
}
