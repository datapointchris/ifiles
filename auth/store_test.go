package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// fakeKeyring stands in for the OS keychain. go-keyring's own MockInit is
// process-global and races under t.Parallel(), which is the reason TokenStore
// takes a backend at all.
type fakeKeyring struct {
	entries map[string]string
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{entries: map[string]string{}} }

func (f *fakeKeyring) Set(service, user, password string) error {
	f.entries[service+"/"+user] = password
	return nil
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	value, ok := f.entries[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	key := service + "/" + user
	if _, ok := f.entries[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, key)
	return nil
}

func newTestStore() (*TokenStore, *fakeKeyring) {
	fake := newFakeKeyring()
	return &TokenStore{backend: fake}, fake
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore()
	const url = "https://files.example.com"

	if err := store.Save(url, "secret-token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	got, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "secret-token" {
		t.Errorf("Load() = %q, want the saved token", got)
	}
}

func TestLoadWithoutATokenReportsNotLoggedIn(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore()
	_, err := store.Load("https://files.example.com")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() error = %v, want ErrNotLoggedIn", err)
	}
}

func TestEnvironmentTokenWinsOverTheKeyring(t *testing.T) {
	store, _ := newTestStore()
	const url = "https://files.example.com"
	if err := store.Save(url, "keyring-token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A CI job or a one-off script supplies the token per-invocation without
	// touching the keychain, which may not even be unlocked.
	t.Setenv("IFILES_TOKEN", "  env-token  ")

	got, err := store.Load(url)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "env-token" {
		t.Errorf("Load() = %q, want the trimmed environment token", got)
	}
}

func TestTokensAreKeyedPerServer(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore()
	if err := store.Save("https://one.example.com", "token-one"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if err := store.Save("https://two.example.com", "token-two"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	first, err := store.Load("https://one.example.com")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if first != "token-one" {
		t.Errorf("first server token = %q, want token-one; a second login overwrote it", first)
	}
}

func TestKeyNormalizationFindsTheSameToken(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore()
	if err := store.Save("https://Files.Example.com/", "token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A trailing slash or different casing typed at login must not strand the
	// token under a second keychain entry that nothing later looks up.
	got, err := store.Load("https://files.example.com")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != "token" {
		t.Errorf("Load() = %q, want the token saved under the unnormalized URL", got)
	}
}

func TestDeleteRemovesTheToken(t *testing.T) {
	t.Setenv("IFILES_TOKEN", "")

	store, _ := newTestStore()
	const url = "https://files.example.com"
	if err := store.Save(url, "token"); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if err := store.Delete(url); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	if _, err := store.Load(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("Load() after Delete() error = %v, want ErrNotLoggedIn", err)
	}
	if err := store.Delete(url); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("second Delete() error = %v, want ErrNotLoggedIn", err)
	}
}
