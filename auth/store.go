// Package auth persists FileBrowser API tokens, preferring the OS keyring and
// falling back to a 0600 file on hosts that have no keyring at all.
package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// keyringService namespaces ifiles' entries in the OS keychain. Tokens are keyed
// within it by server URL, so a second FileBrowser instance does not overwrite
// the first — the keychain is Syncthing-adjacent across machines and a single
// shared key would make two hosts fight over one entry.
const keyringService = "ifiles"

// ErrNotLoggedIn is returned when no token is stored for a server.
var ErrNotLoggedIn = errors.New("not logged in: run `ifiles auth login`")

// Backend names where a token came from or went. Two stores mean "where is my
// token" has an answer worth reporting: `auth login` says when it had to fall
// back, and `auth status` says which one is in play.
type Backend string

const (
	BackendEnv     Backend = "IFILES_TOKEN"
	BackendKeyring Backend = "OS keyring"
	BackendFile    Backend = "file"
)

// keyringBackend is the seam that lets tests swap the OS keychain for an
// in-memory fake. go-keyring's MockInit is process-global and races under
// t.Parallel(), so the store depends on this interface instead.
type keyringBackend interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (osKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

func (osKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// TokenStore persists API tokens, in the OS keychain wherever there is one.
type TokenStore struct {
	keyring keyringBackend
	file    *fileStore
}

func NewTokenStore() *TokenStore {
	return &TokenStore{keyring: osKeyring{}, file: newFileStore(DefaultTokenPath())}
}

// FilePath reports the fallback file, so a caller told the token went there can
// name it.
func (t *TokenStore) FilePath() string { return t.file.Path() }

// Save stores the token and reports which backend took it.
//
// On Linux the keyring is the Secret Service over D-Bus, and a host without one
// fails before ifiles reaches the network — a bare Ubuntu userland answers
// `exec: "dbus-launch": executable file not found in $PATH`. Refusing to store a
// token there would make the tool unusable on exactly the machines whose only
// other route to the server is a browser. The token goes to a 0600 file instead,
// and the backend is returned rather than swallowed: a downgrade from keychain to
// plaintext is not something to find out about later.
func (t *TokenStore) Save(serverURL, token string) (Backend, error) {
	keyringErr := t.keyring.Set(keyringService, key(serverURL), token)
	if keyringErr == nil {
		return BackendKeyring, nil
	}
	if err := t.file.Set(key(serverURL), token); err != nil {
		return "", fmt.Errorf("the OS keyring is unavailable (%v), and the fallback file failed: %w", keyringErr, err)
	}
	return BackendFile, nil
}

// Load resolves the token for a server, reporting where it came from.
//
// IFILES_TOKEN wins so a script or a CI job can supply one per-invocation
// without touching either store; there is deliberately no --token flag, because
// a flag value lands in ps output and shell history.
func (t *TokenStore) Load(serverURL string) (string, Backend, error) {
	if v := strings.TrimSpace(os.Getenv("IFILES_TOKEN")); v != "" {
		return v, BackendEnv, nil
	}

	// The keyring wins whenever it actually holds something, so a host that gains
	// a Secret Service later moves onto it without a re-login.
	token, keyringErr := t.keyring.Get(keyringService, key(serverURL))
	if keyringErr == nil && strings.TrimSpace(token) != "" {
		return token, BackendKeyring, nil
	}

	token, err := t.file.Get(key(serverURL))
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(token) != "" {
		return token, BackendFile, nil
	}

	// Nothing anywhere. When the keyring failed for a reason other than an absent
	// entry, that reason is the actionable part: on a host with no Secret Service
	// the fix is `auth login`, which will use the file, but on a machine that has
	// one it is an unlocked keychain — and a bare ErrNotLoggedIn sends the user off
	// to re-mint a token that was never the problem.
	if keyringErr != nil && !errors.Is(keyringErr, keyring.ErrNotFound) {
		return "", "", fmt.Errorf("%w (the OS keyring is also unavailable: %v)", ErrNotLoggedIn, keyringErr)
	}
	return "", "", ErrNotLoggedIn
}

// Delete forgets the token in both stores rather than in the first one that
// answers. A token written to the file before that host had a keyring would
// otherwise survive a logout and keep authenticating.
func (t *TokenStore) Delete(serverURL string) error {
	keyringErr := t.keyring.Delete(keyringService, key(serverURL))
	fileErr := t.file.Delete(key(serverURL))

	if fileErr != nil && !errors.Is(fileErr, ErrNotLoggedIn) {
		return fileErr
	}
	if keyringErr == nil || fileErr == nil {
		return nil
	}
	return ErrNotLoggedIn
}

// key normalizes the server URL into a keychain account name, so a trailing
// slash or a differently-cased host does not strand a token under a second key.
func key(serverURL string) string {
	return strings.ToLower(strings.TrimRight(serverURL, "/"))
}
