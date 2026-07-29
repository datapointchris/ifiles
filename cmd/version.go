package cmd

import (
	"runtime/debug"
	"strings"

	"github.com/datapointchris/goselfupdate"
	"github.com/spf13/cobra"
)

// Set via -ldflags at build time by goreleaser.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// buildVersion reports the running binary's version. `go install pkg@latest`
// applies no ldflags but does stamp the module version into build info, so
// without this fallback every installed copy identifies as a dev build and
// `ifiles update` refuses to run.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return resolveVersion(version, info.Main.Version)
}

func resolveVersion(ldflagsVersion, moduleVersion string) string {
	if ldflagsVersion != "dev" && ldflagsVersion != "" {
		return ldflagsVersion
	}
	// Go stamps a VCS-derived pseudo-version onto local `go build` output and that
	// string is valid semver, which is why the release check lives in
	// goselfupdate rather than being re-derived here.
	if !goselfupdate.IsReleaseVersion(moduleVersion) {
		return ldflagsVersion
	}
	return strings.TrimPrefix(moduleVersion, "v")
}

var versionCmd = &cobra.Command{
	Use:     "version",
	GroupID: groupTool,
	Short:   "Print ifiles version information",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		current := buildVersion()
		outf(cmd, "ifiles %s", current)
		outf(cmd, "  commit: %s", commit)
		outf(cmd, "  built:  %s", date)
		if current == "dev" {
			if info, ok := debug.ReadBuildInfo(); ok {
				outf(cmd, "  go:     %s", info.GoVersion)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	rootCmd.Version = buildVersion()
	rootCmd.SetVersionTemplate("ifiles {{.Version}}\n")

	// Free -v for a future --verbose flag, matching forge, bbkt, nomad, and meso.
	rootCmd.InitDefaultVersionFlag()
	if f := rootCmd.Flags().Lookup("version"); f != nil {
		f.Shorthand = ""
	}
}
