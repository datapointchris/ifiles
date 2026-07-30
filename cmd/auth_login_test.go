package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A first login has no configured URL, so the generic "no server configured"
// remedy points at `ifiles auth login` — the command already being run. The
// login path has to name the missing argument instead.
func TestLoginWithoutAURLNamesTheArgumentRatherThanItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	t.Setenv("IFILES_URL", "")

	previous := configPath
	configPath = path
	t.Cleanup(func() { configPath = previous })

	err := authLoginCmd.RunE(authLoginCmd, nil)
	if err == nil {
		t.Fatal("auth login with no URL succeeded, want an error")
	}
	if strings.Contains(err.Error(), "no server configured") {
		t.Errorf("error %q is the generic remedy, which tells the user to run the command that failed", err)
	}
	if !strings.Contains(err.Error(), "auth login https://") {
		t.Errorf("error %q does not show the URL argument", err)
	}
}
