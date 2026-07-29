package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/auth"
	"github.com/datapointchris/ifiles/filebrowser"
)

var statusJSON bool

// statusReport is the --json shape. Reachable and Authenticated are separate
// fields because they fail for unrelated reasons and the fix differs: a down
// server needs waiting, a dead token needs re-minting.
type statusReport struct {
	Server        string `json:"server"`
	Source        string `json:"source"`
	Config        string `json:"config"`
	HasToken      bool   `json:"hasToken"`
	TokenFrom     string `json:"tokenFrom,omitempty"`
	Reachable     bool   `json:"reachable"`
	Authenticated bool   `json:"authenticated"`
	Error         string `json:"error,omitempty"`
}

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
			return finishStatus(cmd, report, errors.New("no server configured: run `ifiles auth login`"))
		}

		token, backend, tokenErr := auth.NewTokenStore().Load(cfg.URL)
		report.HasToken = tokenErr == nil
		report.TokenFrom = string(backend)

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
		outf(cmd, "Config:        %s", report.Config)

		table := newTable(cmd.OutOrStdout())
		writef(table, "\nSOURCE\tSTATUS\tFILES\tDIRS\tREAD-ONLY\n")
		for _, source := range sources {
			writef(table, "%s\t%s\t%d\t%d\t%t\n",
				source.Name, source.Status, source.NumFiles, source.NumDirs, source.ReadOnly)
		}
		return table.Flush()
	},
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
