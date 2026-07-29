// Package config reads and writes ifiles' TOML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultChunkSize sits well under the 100 MB request-body cap on Cloudflare's
// free tier, which every upload to files.ichrisbirch.com passes through. It is
// not a throughput tuning knob: exceeding the cap fails the request outright
// with a 413 that no retry can clear.
const DefaultChunkSize = 32 << 20

// Config is the on-disk shape of $XDG_CONFIG_HOME/ifiles/config.toml.
type Config struct {
	URL       string `toml:"url"`
	Source    string `toml:"source"`
	RemoteDir string `toml:"remote_dir,omitempty"`
	ChunkSize string `toml:"chunk_size,omitempty"`

	// path is where this config was read from, so Save and every error message
	// name the file the user actually has to edit rather than the default.
	path string
}

// DefaultPath reports where ifiles reads its config, honoring XDG_CONFIG_HOME.
//
// The variable is read directly rather than through os.UserConfigDir, which on
// macOS answers ~/Library/Application Support and ignores XDG entirely. This tool
// runs on both macOS and Arch against a config directory that is shared between
// them, so the path has to be the same on both.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "ifiles", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "ifiles", "config.toml")
	}
	return filepath.Join(home, ".config", "ifiles", "config.toml")
}

// Load reads the config file if present and applies environment overrides. An
// empty override uses DefaultPath. A missing file is not an error: `ifiles auth
// login` writes one, and every field has an environment equivalent so a scripted
// caller needs no file at all.
func Load(override string) (*Config, error) {
	path := override
	if path == "" {
		path = DefaultPath()
	}
	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// An explicit --config that does not exist is a mistake worth reporting;
		// a missing default file is the state before `auth login`.
		if override != "" {
			return nil, fmt.Errorf("config file %s does not exist", path)
		}
	default:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if v := os.Getenv("IFILES_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("IFILES_SOURCE"); v != "" {
		cfg.Source = v
	}
	if v := os.Getenv("IFILES_CHUNK_SIZE"); v != "" {
		cfg.ChunkSize = v
	}

	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return cfg, nil
}

// Path reports the file this config was read from.
func (c *Config) Path() string { return c.path }

// Save writes the config back, creating the directory if needed. `auth login`
// uses this to persist the source name it discovers, so the source query
// parameter that every resource call requires does not have to be typed.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(c.path), err)
	}

	file, err := os.OpenFile(c.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", c.path, err)
	}
	defer func() { _ = file.Close() }()

	if err := toml.NewEncoder(file).Encode(c); err != nil {
		return fmt.Errorf("encoding %s: %w", c.path, err)
	}
	return file.Close()
}

// Chunks reports the upload chunk size in bytes.
func (c *Config) Chunks() (int64, error) {
	if c.ChunkSize == "" {
		return DefaultChunkSize, nil
	}
	size, err := ParseSize(c.ChunkSize)
	if err != nil {
		return 0, fmt.Errorf("chunk_size in %s: %w", c.path, err)
	}
	return size, nil
}

// RequireURL returns the configured base URL, or an error naming every way to
// supply one.
func (c *Config) RequireURL() (string, error) {
	if c.URL == "" {
		return "", fmt.Errorf("no server configured: run `ifiles auth login`, set url in %s, or set IFILES_URL", c.path)
	}
	return c.URL, nil
}
