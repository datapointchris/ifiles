// Package auth persists FileBrowser API tokens in the OS keyring.
package auth

import (
	"errors"
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

// TokenStore persists API tokens in the OS keychain.
type TokenStore struct {
	backend keyringBackend
}

func NewTokenStore() *TokenStore { return &TokenStore{backend: osKeyring{}} }

func (t *TokenStore) Save(serverURL, token string) error {
	return t.backend.Set(keyringService, key(serverURL), token)
}

// Load resolves the token for a server. IFILES_TOKEN wins so a script or a CI
// job can supply one per-invocation without touching the keychain; there is
// deliberately no --token flag, because a flag value lands in ps output and
// shell history.
func (t *TokenStore) Load(serverURL string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("IFILES_TOKEN")); v != "" {
		return v, nil
	}
	token, err := t.backend.Get(keyringService, key(serverURL))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotLoggedIn
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", ErrNotLoggedIn
	}
	return token, nil
}

func (t *TokenStore) Delete(serverURL string) error {
	err := t.backend.Delete(keyringService, key(serverURL))
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotLoggedIn
	}
	return err
}

// key normalizes the server URL into a keychain account name, so a trailing
// slash or a differently-cased host does not strand a token under a second key.
func key(serverURL string) string {
	return strings.ToLower(strings.TrimRight(serverURL, "/"))
}
