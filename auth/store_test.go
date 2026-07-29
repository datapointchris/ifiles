package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// errNoKeyring stands in for what a host with no Secret Service actually returns.
// The real message from a bare Ubuntu userland is `exec: "dbus-launch":
// executable file not found in $PATH`, which is neither ErrNotFound nor anything
// a caller could act on — the point of the fallback is that it is not mistaken
// for an empty keyring.
var errNoKeyring = errors.New(`exec: "dbus-launch": executable file not found in $PATH`)

// fakeKeyring stands in for the OS keychain. go-keyring's own MockInit is
// process-global and races under t.Parallel(), which is the reason TokenStore
// takes a backend at all.
type fakeKeyring struct {
	entries     map[string]string
	unavailable bool
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{entries: map[string]string{}} }

func (f *fakeKeyring) Set(service, user, password string) error {
	if f.unavailable {
		return errNoKeyring
	}
	f.entries[service+"/"+user] = password
	return nil
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.unavailable {
		return "", errNoKeyring
	}
	value, ok := f.entries[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	if f.unavailable {
		return errNoKeyring
	}
	key := service + "/" + user
	if _, ok := f.entries[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, key)
	return nil
}

func newTestStore(t *testing.T) (*TokenStore, *fakeKeyring) {
	t.Helper()
	fake := newFakeKeyring()
	store := &TokenStore{
		keyring: fake,
		file:    newFileStore(filepath.Join(t.TempDir(), "token.json")),
	}
	return store, fake
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore(t)
	const url = "https://files.example.com"

	backend, err := store.Save(url, "secret-token")
	if err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if backend != BackendKeyring {
		t.Errorf("Save() backend = %q, want the keyring when one is available", backend)
	}

	got, backend, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "secret-token" {
		t.Errorf("Load() = %q, want the saved token", got)
	}
	if backend != BackendKeyring {
		t.Errorf("Load() backend = %q, want the keyring", backend)
	}
}

func TestLoadWithoutATokenReportsNotLoggedIn(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore(t)
	_, _, err := store.Load("https://files.example.com")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() error = %v, want ErrNotLoggedIn", err)
	}
}

func TestEnvironmentTokenWinsOverTheKeyring(t *testing.T) {
	store, _ := newTestStore(t)
	const url = "https://files.example.com"
	if _, err := store.Save(url, "keyring-token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A CI job or a one-off script supplies the token per-invocation without
	// touching the keychain, which may not even be unlocked.
	t.Setenv("IFILES_TOKEN", "  env-token  ")

	got, backend, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "env-token" {
		t.Errorf("Load() = %q, want the trimmed environment token", got)
	}
	if backend != BackendEnv {
		t.Errorf("Load() backend = %q, want %q", backend, BackendEnv)
	}
}

func TestTokensAreKeyedPerServer(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore(t)
	if _, err := store.Save("https://one.example.com", "token-one"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if _, err := store.Save("https://two.example.com", "token-two"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	first, _, err := store.Load("https://one.example.com")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if first != "token-one" {
		t.Errorf("first server token = %q, want token-one; a second login overwrote it", first)
	}
}

func TestKeyNormalizationFindsTheSameToken(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore(t)
	if _, err := store.Save("https://Files.Example.com/", "token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A trailing slash or different casing typed at login must not strand the
	// token under a second keychain entry that nothing later looks up.
	got, _, err := store.Load("https://files.example.com")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "token" {
		t.Errorf("Load() = %q, want the token saved under the unnormalized URL", got)
	}
}

func TestDeleteRemovesTheToken(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore(t)
	const url = "https://files.example.com"
	if _, err := store.Save(url, "token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if err := store.Delete(url); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	if _, _, err := store.Load(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() after Delete() error = %v, want ErrNotLoggedIn", err)
	}
	if err := store.Delete(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("second Delete() error = %v, want ErrNotLoggedIn", err)
	}
}

// TestKeyringlessHostRoundTrip is the work-box case: WSL with no Secret Service,
// where every command used to fail on a dbus-launch that is not installed.
func TestKeyringlessHostRoundTrip(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	fake.unavailable = true
	const url = "https://files.example.com"

	backend, err := store.Save(url, "secret-token")
	if err != nil {
		t.Fatalf("Save() returned error with no keyring available: %v", err)
	}
	if backend != BackendFile {
		t.Errorf("Save() backend = %q, want %q", backend, BackendFile)
	}

	got, backend, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "secret-token" {
		t.Errorf("Load() = %q, want the token written to the fallback file", got)
	}
	if backend != BackendFile {
		t.Errorf("Load() backend = %q, want %q", backend, BackendFile)
	}

	if err := store.Delete(url); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	if _, _, err := store.Load(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() after Delete() error = %v, want ErrNotLoggedIn", err)
	}
}

// A token is a secret even when the keyring cannot hold it. Group or world read
// on the fallback file would make the downgrade a real exposure rather than a
// documented one.
func TestFallbackTokenFileIsPrivate(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	fake.unavailable = true
	if _, err := store.Save("https://files.example.com", "secret-token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	info, err := os.Stat(store.FilePath())
	if err != nil {
		t.Fatalf("stat of the token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %v, want 0600", perm)
	}
}

// A host that gains a Secret Service must not appear logged out, which is what
// preferring the keyring would cause if it stopped at "the keyring is empty".
func TestKeyringTakesPrecedenceButAFileTokenStillWorks(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	const url = "https://files.example.com"

	fake.unavailable = true
	if _, err := store.Save(url, "file-token"); err != nil {
		t.Fatalf("Save() to the file returned error: %v", err)
	}

	fake.unavailable = false
	got, backend, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "file-token" || backend != BackendFile {
		t.Errorf("Load() = %q from %q, want the file token an empty keyring cannot supply", got, backend)
	}

	if _, err := store.Save(url, "keyring-token"); err != nil {
		t.Fatalf("Save() to the keyring returned error: %v", err)
	}
	got, backend, err = store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "keyring-token" || backend != BackendKeyring {
		t.Errorf("Load() = %q from %q, want the keyring to win once it holds a token", got, backend)
	}
}

// An unreachable keyring and no file token is not the same situation as a plain
// missing login: on a machine that does have a keychain, the fix is unlocking it,
// and a bare "not logged in" sends the user to re-mint a working token.
func TestLoadReportsWhyTheKeyringFailedWhenNothingIsStored(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	fake.unavailable = true

	_, _, err := store.Load("https://files.example.com")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Load() error = %v, want it to wrap ErrNotLoggedIn", err)
	}
	if !strings.Contains(err.Error(), "dbus-launch") {
		t.Errorf("Load() error = %q, want the keyring failure named in it", err)
	}
}

// A logout has to clear both stores. Leaving one behind means a token that keeps
// authenticating after the user believes it is gone.
func TestDeleteClearsBothStores(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	const url = "https://files.example.com"

	fake.unavailable = true
	if _, err := store.Save(url, "file-token"); err != nil {
		t.Fatalf("Save() to the file returned error: %v", err)
	}
	fake.unavailable = false
	if _, err := store.Save(url, "keyring-token"); err != nil {
		t.Fatalf("Save() to the keyring returned error: %v", err)
	}

	if err := store.Delete(url); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	if _, _, err := store.Load(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() after Delete() error = %v, want ErrNotLoggedIn", err)
	}

	fake.unavailable = true
	if _, _, err := store.Load(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() from the file after Delete() error = %v, want ErrNotLoggedIn", err)
	}
}

// A corrupt token file must not read as "not logged in": that would send the user
// to mint a new token, and the next login would overwrite whatever is in there.
func TestCorruptTokenFileIsReportedNotIgnored(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, fake := newTestStore(t)
	fake.unavailable = true
	if err := os.MkdirAll(filepath.Dir(store.FilePath()), 0o700); err != nil {
		t.Fatalf("creating the state directory: %v", err)
	}
	if err := os.WriteFile(store.FilePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the corrupt file: %v", err)
	}

	_, _, err := store.Load("https://files.example.com")
	if err == nil {
		t.Fatal("Load() returned no error for a corrupt token file")
	}
	if errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() error = %v, want a parse failure rather than ErrNotLoggedIn", err)
	}
}

func TestDefaultTokenPathHonorsXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	want := filepath.Join(state, "ifiles", "token.json")
	if got := DefaultTokenPath(); got != want {
		t.Errorf("DefaultTokenPath() = %q, want %q", got, want)
	}
}
