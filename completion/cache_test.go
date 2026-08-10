package completion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/datapointchris/ifiles/v3/filebrowser"
)

func cacheAt(t *testing.T, ttl time.Duration) *Cache {
	t.Helper()
	return NewCacheAt(filepath.Join(t.TempDir(), "listings.json"), ttl)
}

func photos() []filebrowser.Item {
	return []filebrowser.Item{
		{Name: "raw", Type: filebrowser.DirectoryType},
		{Name: "sunset.jpg", Type: "image/jpeg", Size: 4 << 20},
	}
}

func TestCacheReturnsAStoredListing(t *testing.T) {
	t.Parallel()

	cache := cacheAt(t, DefaultTTL)
	key := Key("https://files.example.com", "files", "/photos")
	if err := cache.Store(key, photos()); err != nil {
		t.Fatalf("Store() returned error: %v", err)
	}

	items, ok := cache.Lookup(key)
	if !ok {
		t.Fatal("Lookup() found nothing just after Store()")
	}
	if len(items) != 2 || items[0].Name != "raw" || !items[0].IsDir() {
		t.Errorf("Lookup() = %+v, want the stored listing with its types intact", items)
	}
}

func TestCacheIgnoresAnExpiredListing(t *testing.T) {
	t.Parallel()

	// Zero TTL: anything already written is stale, which is the same code path a
	// listing from ten seconds ago takes without the wait.
	cache := cacheAt(t, 0)
	key := Key("https://files.example.com", "files", "/photos")
	if err := cache.Store(key, photos()); err != nil {
		t.Fatalf("Store() returned error: %v", err)
	}

	if _, ok := cache.Lookup(key); ok {
		t.Error("Lookup() served an expired listing")
	}
}

func TestCacheSeparatesSourcesAndServers(t *testing.T) {
	t.Parallel()

	// The same path names different files under a different source, and --source
	// and IFILES_URL can both change between two invocations a second apart.
	cache := cacheAt(t, DefaultTTL)
	if err := cache.Store(Key("https://a.example.com", "files", "/"), photos()); err != nil {
		t.Fatalf("Store() returned error: %v", err)
	}

	if _, ok := cache.Lookup(Key("https://a.example.com", "backup", "/")); ok {
		t.Error("a listing cached for one source answered for another")
	}
	if _, ok := cache.Lookup(Key("https://b.example.com", "files", "/")); ok {
		t.Error("a listing cached for one server answered for another")
	}
}

func TestCacheDropsExpiredEntriesOnWrite(t *testing.T) {
	t.Parallel()

	// Nothing else ever deletes an entry, so without pruning here a walk through a
	// deep tree leaves a file that grows by a directory per Tab, forever.
	cache := cacheAt(t, 0)
	for _, dir := range []string{"/a", "/b", "/c"} {
		if err := cache.Store(Key("https://files.example.com", "files", dir), photos()); err != nil {
			t.Fatalf("Store() returned error: %v", err)
		}
	}

	data, err := os.ReadFile(cache.Path())
	if err != nil {
		t.Fatalf("reading cache: %v", err)
	}
	stored := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("parsing cache: %v", err)
	}
	if len(stored) != 1 {
		t.Errorf("cache holds %d entries, want only the newest", len(stored))
	}
}

func TestCacheTreatsACorruptFileAsEmpty(t *testing.T) {
	t.Parallel()

	// A cache is worth exactly one request to replace. Reporting the corruption
	// would turn a recoverable state into a completion that never works again.
	cache := cacheAt(t, DefaultTTL)
	if err := os.WriteFile(cache.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing cache: %v", err)
	}

	if _, ok := cache.Lookup(Key("https://files.example.com", "files", "/")); ok {
		t.Error("Lookup() returned something from a corrupt cache")
	}
	if err := cache.Store(Key("https://files.example.com", "files", "/"), photos()); err != nil {
		t.Errorf("Store() over a corrupt cache returned error: %v", err)
	}
}

func TestCacheWritesOwnerOnly(t *testing.T) {
	t.Parallel()

	// Filenames are not secrets, but they are the shape of a private filesystem,
	// and every other file this tool writes is 0600.
	cache := cacheAt(t, DefaultTTL)
	if err := cache.Store(Key("https://files.example.com", "files", "/"), photos()); err != nil {
		t.Fatalf("Store() returned error: %v", err)
	}

	info, err := os.Stat(cache.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache file mode = %o, want 600", perm)
	}
}

func TestDefaultPathHonorsXDGCacheHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	if got, want := DefaultPath(), filepath.Join(dir, "ifiles", "listings.json"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
