package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// usePad sizes the command column from the full `Use` string rather than the bare
// command name. Cobra's default lists `get`; this lists `get <remote> [local]`,
// so which side of a verb is remote and which is local is visible from the parent
// screen instead of one drill-down away — and that asymmetry is the whole grammar
// here.
func usePad(cmds []*cobra.Command) int {
	width := 0
	for _, c := range cmds {
		if !c.IsAvailableCommand() && c.Name() != "help" {
			continue
		}
		if n := len(c.Use); n > width {
			width = n
		}
	}
	// +1 so the longest row still gets a two-space gutter, matching the tables
	// this tool prints elsewhere. Cobra's own padding leaves it with one.
	return width + 1
}

// The stock cobra template with the three command-list rows rewritten to use
// `Use` and the computed width. Everything else is cobra's own, so an upgrade
// that changes the surrounding structure is a visible diff rather than a silent
// divergence.
const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{$pad := usePad .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Use $pad}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Use $pad}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Use $pad}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

func init() {
	cobra.AddTemplateFunc("usePad", usePad)
	rootCmd.SetUsageTemplate(strings.TrimPrefix(usageTemplate, "\n"))
}
