// Package xdg resolves the directories ifiles keeps its own files in, under the
// XDG base directories.
//
// Each variable is read directly rather than through os.UserConfigDir or
// os.UserCacheDir. Those answer ~/Library/Application Support and ~/Library/Caches
// on macOS and ignore XDG entirely, so a config directory shared between macOS and
// Linux would resolve to different paths on each.
package xdg

import (
	"os"
	"path/filepath"
)

// app is the directory name ifiles owns under each base directory.
const app = "ifiles"

// ConfigDir is $XDG_CONFIG_HOME/ifiles, else ~/.config/ifiles.
func ConfigDir() (string, error) { return dir("XDG_CONFIG_HOME", ".config") }

// StateDir is $XDG_STATE_HOME/ifiles, else ~/.local/state/ifiles.
func StateDir() (string, error) { return dir("XDG_STATE_HOME", ".local", "state") }

// CacheDir is $XDG_CACHE_HOME/ifiles, else ~/.cache/ifiles.
func CacheDir() (string, error) { return dir("XDG_CACHE_HOME", ".cache") }

// dir returns the error os.UserHomeDir gives when the variable is unset and there
// is no home directory. Each caller decides its own fallback then, because a
// secret and a cache tolerate different places.
func dir(env string, homeRelative ...string) (string, error) {
	if base := os.Getenv(env); base != "" {
		return filepath.Join(base, app), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append(append([]string{home}, homeRelative...), app)...), nil
}
