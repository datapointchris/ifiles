package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsAndRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	original := &Config{
		URL:       "https://files.example.com",
		Source:    "files",
		RemoteDir: "/inbox",
		ChunkSize: "8MiB",
		path:      path,
	}
	if err := original.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if loaded.URL != original.URL || loaded.Source != original.Source {
		t.Errorf("Load() = %+v, want URL and Source from %+v", loaded, original)
	}
	if loaded.RemoteDir != original.RemoteDir || loaded.ChunkSize != original.ChunkSize {
		t.Errorf("Load() = %+v, want RemoteDir and ChunkSize from %+v", loaded, original)
	}
}

func TestSaveWritesOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &Config{URL: "https://files.example.com", path: path}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}
	// The config holds no secret today, but it names the server a token belongs to
	// and lives beside state that does.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %v, want 0600", perm)
	}
}

func TestLoadTrimsTrailingSlashFromURL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`url = "https://files.example.com/"`), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	// Every request appends /api to this, so a trailing slash would produce
	// //api and a 404 that reads like a routing bug.
	if cfg.URL != "https://files.example.com" {
		t.Errorf("URL = %q, want the trailing slash removed", cfg.URL)
	}
}

func TestLoadExplicitMissingPathIsAnError(t *testing.T) {
	t.Parallel()

	// A --config the user typed and got wrong deserves a report; a missing default
	// file is just the state before `auth login`.
	if _, err := Load(filepath.Join(t.TempDir(), "absent", "config.toml")); err == nil {
		t.Fatal("Load() with an explicit missing path succeeded, want an error")
	}
}

func TestLoadMissingDefaultIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("IFILES_URL", "")
	t.Setenv("IFILES_SOURCE", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() with no config file returned error: %v", err)
	}
	if cfg.URL != "" {
		t.Errorf("URL = %q, want empty from a fresh machine", cfg.URL)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("url = \"https://file.example.com\"\nsource = \"from-file\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	t.Setenv("IFILES_URL", "https://env.example.com")
	t.Setenv("IFILES_SOURCE", "from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.URL != "https://env.example.com" || cfg.Source != "from-env" {
		t.Errorf("Load() = %+v, want the environment to win over the file", cfg)
	}
}

func TestRequireURLNamesEveryWayToSupplyOne(t *testing.T) {
	t.Parallel()

	cfg := &Config{path: "/tmp/ifiles/config.toml"}
	_, err := cfg.RequireURL()
	if err == nil {
		t.Fatal("RequireURL() with no URL succeeded, want an error")
	}
	for _, want := range []string{"auth login", "IFILES_URL", cfg.path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDefaultPathHonorsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	// os.UserConfigDir answers ~/Library/Application Support on macOS and ignores
	// XDG, which would split this tool's config across the two machines it runs on.
	want := "/custom/config/ifiles/config.toml"
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestDefaultPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got := DefaultPath()
	if !strings.HasSuffix(got, filepath.Join(".config", "ifiles", "config.toml")) {
		t.Errorf("DefaultPath() = %q, want it under ~/.config", got)
	}
}
