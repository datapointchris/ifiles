package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/auth"
	"github.com/datapointchris/ifiles/filebrowser"
)

var statusJSON bool

// statusReport is the --json shape. Reachable and Authenticated are separate
// fields because they fail for unrelated reasons and the fix differs: a down
// server needs waiting, a dead token needs re-minting.
type statusReport struct {
	Server    string `json:"server"`
	Source    string `json:"source"`
	Config    string `json:"config"`
	HasToken  bool   `json:"hasToken"`
	TokenFrom string `json:"tokenFrom,omitempty"`
	// TokenExpires is read out of the stored token rather than from the server,
	// so it is populated even when the server is unreachable. Empty when the
	// token is not a readable JWT, which is worth showing as a blank rather than
	// as an error: the token may still work.
	TokenExpires  string `json:"tokenExpires,omitempty"`
	Reachable     bool   `json:"reachable"`
	Authenticated bool   `json:"authenticated"`
	Error         string `json:"error,omitempty"`
}

// tokenExpiryWarning is how close to expiry `auth status` starts saying so. A
// token is minted for months at a time and replacing one means a browser
// session, so the window has to be long enough to be a task rather than an
// interruption.
const tokenExpiryWarning = 14 * 24 * time.Hour

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report whether the server is reachable and the token valid",
	Long: `Reports the configured server, the stored token, and what the server says.

Reachability and authentication are checked separately, because one 401 cannot
tell you whether the server is down or the token has expired, and those have
different fixes.`,
	Example: `  ifiles auth status
  ifiles auth status --json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		report := statusReport{Server: cfg.URL, Source: cfg.Source, Config: cfg.Path()}
		if cfg.URL == "" {
			return finishStatus(cmd, report, errors.New("no server configured: run `ifiles auth login <url>`"))
		}

		token, backend, tokenErr := auth.NewTokenStore().Load(cfg.URL)
		report.HasToken = tokenErr == nil
		report.TokenFrom = string(backend)

		// Read before any network call, so the expiry is still reported when the
		// server is down — which is exactly when someone is trying to work out
		// whether the problem is theirs.
		expiry, expiryErr := auth.ExpiresAt(token)
		if expiryErr == nil {
			report.TokenExpires = expiry.Format(time.RFC3339)
		}

		client, err := filebrowser.New(filebrowser.Options{BaseURL: cfg.URL, Token: token, Source: cfg.Source})
		if err != nil {
			return finishStatus(cmd, report, err)
		}

		ctx, cancel := commandContext(cmd)
		defer cancel()

		if err := client.Health(ctx); err != nil {
			return finishStatus(cmd, report, err)
		}
		report.Reachable = true

		if tokenErr != nil {
			return finishStatus(cmd, report, tokenErr)
		}

		sources, err := client.Sources(ctx)
		if err != nil {
			// A 401 and a token this client can already see has lapsed are the same
			// failure, and the server's message does not say which. Naming the date
			// turns "rejected" into something with an obvious next step.
			if filebrowser.IsUnauthorized(err) && expiryErr == nil && expiry.Before(time.Now()) {
				return finishStatus(cmd, report, fmt.Errorf(
					"the stored token expired on %s; mint a new one in the web UI under Settings",
					expiry.Local().Format(tokenTimeFormat)))
			}
			return finishStatus(cmd, report, err)
		}
		report.Authenticated = true

		if statusJSON {
			return emitJSON(cmd, report)
		}

		outf(cmd, "Server:        %s", report.Server)
		outf(cmd, "Source:        %s", report.Source)
		outf(cmd, "Authenticated: yes")
		outf(cmd, "Token from:    %s", report.TokenFrom)
		outf(cmd, "Token expires: %s", tokenExpiryText(expiry, expiryErr))
		outf(cmd, "Config:        %s", report.Config)

		if expiryErr == nil {
			if remaining := time.Until(expiry); remaining < tokenExpiryWarning {
				infof(cmd, "\nThat token expires in %s. Mint a replacement in the web UI under", humanizeDays(remaining))
				infof(cmd, "Settings, API tokens, then run `ifiles auth login` to store it.")
			}
		}

		table := newTable(cmd.OutOrStdout())
		writef(table, "\nSOURCE\tSTATUS\tFILES\tDIRS\tREAD-ONLY\n")
		for _, source := range sources {
			writef(table, "%s\t%s\t%d\t%d\t%t\n",
				source.Name, source.Status, source.NumFiles, source.NumDirs, source.ReadOnly)
		}
		return table.Flush()
	},
}

// tokenTimeFormat is a date and minute. A token's lifetime is months, so the
// only thing anyone reads off it is which day to act on.
const tokenTimeFormat = "2006-01-02 15:04"

// tokenExpiryText renders the expiry line, including the two cases where there
// is no date to render. A token that cannot be decoded is reported as unknown
// rather than as an error: it may well still authenticate, and the server is the
// authority on that.
func tokenExpiryText(expiry time.Time, err error) string {
	if err != nil {
		return "unknown (token is not a readable JWT)"
	}
	remaining := time.Until(expiry)
	if remaining <= 0 {
		return fmt.Sprintf("%s (expired)", expiry.Local().Format(tokenTimeFormat))
	}
	return fmt.Sprintf("%s (in %s)", expiry.Local().Format(tokenTimeFormat), humanizeDays(remaining))
}

// humanizeDays renders a remaining lifetime at the resolution it is acted on.
// Below a day the number of hours is what decides whether to fix it now.
func humanizeDays(remaining time.Duration) string {
	if days := int(remaining.Hours() / 24); days >= 1 {
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	hours := int(remaining.Hours())
	if hours <= 1 {
		return "under an hour"
	}
	return fmt.Sprintf("%d hours", hours)
}

// finishStatus reports a failure through whichever channel was asked for. With
// --json the report is still valid output on stdout and the command still exits
// non-zero, so a caller gets both the machine answer and the signal.
func finishStatus(cmd *cobra.Command, report statusReport, cause error) error {
	if !statusJSON {
		return cause
	}
	report.Error = cause.Error()
	if err := emitJSON(cmd, report); err != nil {
		return err
	}
	return cause
}

func init() {
	authStatusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output the status report as JSON to stdout")
	authCmd.AddCommand(authStatusCmd)
}
