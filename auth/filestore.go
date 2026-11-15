package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/datapointchris/ifiles/atomicfile"
)

// DefaultTokenPath reports the fallback token file, honoring XDG_STATE_HOME.
//
// The state directory rather than the config directory: config.toml is a file to
// hand-edit and paste out of, and a secret must not sit one field away from that.
func DefaultTokenPath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "ifiles", "token.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// No home means no private directory to put a secret in. Empty makes the
		// backend refuse, rather than dropping a token into a shared temp
		// directory the way the resume state can safely fall back to.
		return ""
	}
	return filepath.Join(home, ".local", "state", "ifiles", "token.json")
}

// fileStore keeps tokens in a 0600 JSON file, for hosts with no OS keyring at
// all: WSL, containers, any headless server. Keyed by the same
// normalized server URL as the keyring, so the two backends address one token.
type fileStore struct {
	path string
}

func newFileStore(path string) *fileStore { return &fileStore{path: path} }

func (f *fileStore) Path() string { return f.path }

// Get returns the empty string when nothing is stored, leaving "absent" for the
// caller to interpret — it has to weigh a missing file token against whatever the
// keyring said before deciding the user is not logged in.
func (f *fileStore) Get(account string) (string, error) {
	if f.path == "" {
		return "", nil
	}
	tokens, err := f.load()
	if err != nil {
		return "", err
	}
	return tokens[account], nil
}

func (f *fileStore) Set(account, token string) error {
	if f.path == "" {
		return errors.New("no home directory to store a token in: set XDG_STATE_HOME, or pass the token in IFILES_TOKEN")
	}
	tokens, err := f.load()
	if err != nil {
		return err
	}
	tokens[account] = token
	return f.write(tokens)
}

func (f *fileStore) Delete(account string) error {
	if f.path == "" {
		return ErrNotLoggedIn
	}
	tokens, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := tokens[account]; !ok {
		return ErrNotLoggedIn
	}
	delete(tokens, account)
	return f.write(tokens)
}

func (f *fileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.path, err)
	}

	tokens := map[string]string{}
	if err := json.Unmarshal(data, &tokens); err != nil {
		// Unlike the resume state, this cannot be recovered by starting over.
		// Treating a corrupt file as empty would report "not logged in" for a
		// token sitting right there, and the next login would overwrite it.
		return nil, fmt.Errorf("parsing %s: %w", f.path, err)
	}
	return tokens, nil
}

// write replaces the file atomically and owner-only, so an interrupt cannot leave
// a truncated token behind and no other user can read the one it writes.
func (f *fileStore) write(tokens map[string]string) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(f.path, ".token-*.json", data)
}
