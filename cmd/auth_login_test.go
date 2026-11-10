package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A first login has no configured URL, so the generic no-server remedy points at
// `ifiles auth login` — the command already being run. The login path has to
// name the missing argument instead.
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
	if !errors.Is(err, errLoginNeedsURL) {
		t.Errorf("auth login with no URL returned %v, want errLoginNeedsURL", err)
	}
}
