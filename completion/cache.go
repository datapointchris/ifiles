// Package completion caches remote directory listings for shell tab-completion.
//
// Completion is not a long-running process that can hold state: each Tab runs the
// whole binary again as `ifiles __complete get /photos/` and reads candidates from
// its stdout. Nothing survives between those runs, so without a cache every Tab is
// a fresh HTTPS round-trip to the server — and this one is behind a tunnel, where
// that is a few hundred milliseconds of a shell that appears to have hung.
//
// The TTL is deliberately seconds, not minutes. A cached listing is only ever used
// to offer names, so a stale one costs a suggestion that turns into a 404 on the
// command that follows; the window is sized to cover a burst of Tabs on one path
// and expire long before the next command.
package completion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/datapointchris/ifiles/filebrowser"
)

// DefaultTTL is how long a cached listing is offered. Long enough that tabbing
// through one directory hits the server once, short enough that a file uploaded
// in the previous command shows up in the next completion.
const DefaultTTL = 10 * time.Second

// listing is one directory as it was last seen.
type listing struct {
	Items     []filebrowser.Item `json:"items"`
	FetchedAt time.Time          `json:"fetchedAt"`
}

// Cache is a TTL'd map of directory listings on disk. Cache, not state: losing it
// costs one request, which is why it lives under XDG_CACHE_HOME while the resume
// offsets — whose loss costs a whole transfer — live under XDG_STATE_HOME.
type Cache struct {
	path string
	ttl  time.Duration
}

// DefaultPath reports where cached listings live, honoring XDG_CACHE_HOME.
//
// The variable is read directly rather than through os.UserCacheDir, which answers
// ~/Library/Caches on macOS and ignores XDG. Every other ifiles path is resolved
// the same way, so a config directory shared between macOS and Arch resolves
// identically on both.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "ifiles", "listings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ifiles-listings.json")
	}
	return filepath.Join(home, ".cache", "ifiles", "listings.json")
}

func NewCache() *Cache { return &Cache{path: DefaultPath(), ttl: DefaultTTL} }

// NewCacheAt builds a cache over a specific file and TTL, for tests.
func NewCacheAt(path string, ttl time.Duration) *Cache {
	return &Cache{path: path, ttl: ttl}
}

func (c *Cache) Path() string { return c.path }

// Key identifies a directory on a particular server and source. All three matter:
// the same path names different files under a different source, and --source and
// IFILES_URL can both change between two invocations a second apart.
func Key(serverURL, source, remotePath string) string {
	return serverURL + "\x00" + source + "\x00" + remotePath
}

// Lookup returns a cached listing if one is present and still fresh.
func (c *Cache) Lookup(key string) ([]filebrowser.Item, bool) {
	listings, err := c.load()
	if err != nil {
		return nil, false
	}
	cached, ok := listings[key]
	if !ok {
		return nil, false
	}
	if time.Since(cached.FetchedAt) > c.ttl {
		return nil, false
	}
	return cached.Items, true
}

// Store records a listing, dropping any that have expired. Pruning on write is
// what bounds the file: a walk through a deep tree caches a directory per Tab,
// and nothing else ever deletes them.
func (c *Cache) Store(key string, items []filebrowser.Item) error {
	listings, err := c.load()
	if err != nil {
		return err
	}
	for existing, cached := range listings {
		if time.Since(cached.FetchedAt) > c.ttl {
			delete(listings, existing)
		}
	}
	listings[key] = listing{Items: items, FetchedAt: time.Now().UTC()}
	return c.write(listings)
}

func (c *Cache) load() (map[string]listing, error) {
	data, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return map[string]listing{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", c.path, err)
	}

	listings := map[string]listing{}
	if err := json.Unmarshal(data, &listings); err != nil {
		// A corrupt cache is worth exactly one request to replace, so it is
		// discarded rather than reported.
		return map[string]listing{}, nil
	}
	return listings, nil
}

// write replaces the file atomically. A completion runs concurrently with itself —
// zsh re-invokes the binary while a previous Tab may still be writing — and a
// half-written file read by the next Tab would be discarded as corrupt, throwing
// away every cached directory rather than one.
func (c *Cache) write(listings map[string]listing) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(c.path), err)
	}

	data, err := json.Marshal(listings)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(c.path), ".listings-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// Filenames are not secrets, but they are the shape of a private filesystem,
	// and every other file this tool writes is owner-only.
	if err := os.Chmod(tempName, 0o600); err != nil {
		return err
	}
	return os.Rename(tempName, c.path)
}
