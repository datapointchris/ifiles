// Package completion caches remote directory listings for shell tab-completion.
//
// Completion is not a long-running process that can hold state: each Tab runs the
// whole binary again as `ifiles __complete download /photos/` and reads candidates
// from its stdout. Nothing survives between those runs, so without a cache every
// Tab is a fresh HTTPS round-trip to the server. Behind a tunnel that is a few
// hundred milliseconds of a shell that appears to have hung.
//
// The TTL is deliberately seconds, not minutes. A cached listing is only ever used
// to offer names, so a stale one costs a suggestion that turns into a 404 on the
// command that follows; the window is sized to cover a burst of Tabs on one path
// and expire long before the next command.
package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datapointchris/ifiles/atomicfile"
	"github.com/datapointchris/ifiles/filebrowser"
	"github.com/datapointchris/ifiles/xdg"
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

// Cache is a TTL'd set of directory listings on disk, one file per directory.
// Cache, not state: losing it costs one request, which is why it lives under
// XDG_CACHE_HOME while the resume offsets — whose loss costs a whole transfer —
// live under XDG_STATE_HOME.
//
// A file per directory means a Tab reads and parses only the listing it asks
// for, however many other directories are cached, and two Tabs on different
// directories never rewrite each other's listings.
type Cache struct {
	dir string
	ttl time.Duration
	// singleFile is the one-file layout of earlier releases, removed on the
	// next write so it does not sit in the cache directory forever. Empty for
	// a cache built by NewCacheAt.
	singleFile string
}

// DefaultDir reports where cached listings live, honoring XDG_CACHE_HOME.
func DefaultDir() string {
	dir, err := xdg.CacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ifiles-listings")
	}
	return filepath.Join(dir, "listings")
}

// NewCache returns a cache over DefaultDir whose entries live for DefaultTTL.
func NewCache() *Cache {
	dir := DefaultDir()
	return &Cache{dir: dir, ttl: DefaultTTL, singleFile: dir + ".json"}
}

// NewCacheAt builds a cache over a specific directory and TTL, for tests.
func NewCacheAt(dir string, ttl time.Duration) *Cache {
	return &Cache{dir: dir, ttl: ttl}
}

// Dir reports the directory the cache reads and writes.
func (c *Cache) Dir() string { return c.dir }

// file names a key's listing by a hash of the key, because the key holds a URL
// and a path that are not safe as a filename.
func (c *Cache) file(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

// Key identifies a directory on a particular server and source. All three matter:
// the same path names different files under a different source, and --source and
// IFILES_URL can both change between two invocations a second apart.
func Key(serverURL, source, remotePath string) string {
	return serverURL + "\x00" + source + "\x00" + remotePath
}

// Lookup returns a cached listing if one is present and still fresh. A missing,
// unreadable or corrupt file is a miss: a cached listing is worth exactly one
// request to replace, so nothing about it is reported.
func (c *Cache) Lookup(key string) ([]filebrowser.Item, bool) {
	data, err := os.ReadFile(c.file(key))
	if err != nil {
		return nil, false
	}
	var cached listing
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	if time.Since(cached.FetchedAt) > c.ttl {
		return nil, false
	}
	return cached.Items, true
}

// Store records a listing, then removes every other listing that has expired.
// Pruning on write is what bounds the directory: a walk through a deep tree
// caches a directory per Tab, and nothing else ever deletes them. Expiry is read
// from each file's modification time, so pruning parses none of them.
func (c *Cache) Store(key string, items []filebrowser.Item) error {
	if err := c.write(c.file(key), listing{Items: items, FetchedAt: time.Now().UTC()}); err != nil {
		return err
	}
	if c.singleFile != "" {
		_ = os.Remove(c.singleFile)
	}
	c.prune(filepath.Base(c.file(key)))
	return nil
}

// prune removes expired listings other than keep. A name starting with a dot is
// another completion's temp file mid-write, and is left for its rename.
func (c *Cache) prune(keep string) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == keep || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) <= c.ttl {
			continue
		}
		_ = os.Remove(filepath.Join(c.dir, name))
	}
}

// write replaces one listing's file atomically. A completion runs concurrently
// with itself — zsh re-invokes the binary while a previous Tab may still be
// writing — and a half-written file read by the next Tab would be a miss.
//
// It is owner-only too. Filenames are not secrets, but they are the shape of a
// private filesystem, and every other file this tool writes is owner-only.
func (c *Cache) write(path string, cached listing) error {
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(path, ".listing-*.json", data); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
