package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/filebrowser"
)

var sharesCmd = &cobra.Command{
	Use:     "shares",
	GroupID: groupAuth,
	Short:   "Create and revoke public share links",
	Long: `Share links hand a file or directory to someone with no account here.

A link is a hash the server issues, and anyone holding it can read what it points
at, so the two settings worth thinking about are how long it lives and whether it
needs a password. Neither has a default: a link with no --expires never expires,
which is right for a link you will revoke by hand and wrong for one you mail to
someone.

This needs the "share" permission on the account, which is separate from "api"
and from "admin". Without it the server refuses with a bare 403 carrying no
message at all, so these commands say so themselves.`,
	Example: `  ifiles shares create /photos/wedding --expires 7d
  ifiles shares list
  ifiles shares delete /public/share/T7bQ3xk`,
	// Help is never wrong; a bare `ifiles shares` teaches the verbs rather than
	// erroring on a missing subcommand.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// shareError makes the share routes' overloaded 403 readable.
//
// With no message it is the middleware refusing on a missing permission, and the
// advice has to be supplied here because the server supplies nothing. With one it
// is the handler explaining itself — "path not found", "the target source is
// private" — and that sentence is already the whole answer, so the method and
// status prefix in front of it is noise. Everything else passes through untouched.
func shareError(err error) error {
	if filebrowser.IsMissingPermission(err) {
		return errors.New(`this account lacks the "share" permission; an admin grants it in the web UI under Settings, User Management`)
	}
	var apiErr *filebrowser.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && apiErr.Message != "" {
		return errors.New(apiErr.Message)
	}
	return err
}

// shareHash accepts either a bare hash or one of the two URLs a share is known
// by. The URL is what is on the clipboard after sending a link to someone, and
// it is the handle the listing prints, so making it unusable as input would make
// the displayed row unusable as input.
func shareHash(argument string) (string, error) {
	trimmed := strings.TrimSpace(argument)
	if trimmed == "" {
		return "", fmt.Errorf("give the share hash or its URL")
	}

	// The download URL carries the hash in the query string rather than in the
	// path — it is a route on /public/api/resources/download, not on the share —
	// so the path split below would find "download" and delete nothing.
	if parsed, err := url.Parse(trimmed); err == nil {
		if hash := parsed.Query().Get("hash"); hash != "" {
			return hash, nil
		}
	}

	// Hashes are raw-URL-safe base64, so they never contain a slash: whatever
	// follows the last one is the hash, URL or not.
	hash := path.Base(strings.TrimRight(trimmed, "/"))
	if hash == "" || hash == "." {
		return "", fmt.Errorf("no share hash in %q", argument)
	}
	return hash, nil
}

func init() {
	rootCmd.AddCommand(sharesCmd)
}
