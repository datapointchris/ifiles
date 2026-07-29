// Package resume records where an interrupted transfer got to.
//
// Both directions need it, for the same reason: an offset alone is not enough to
// resume safely, because nothing about a partial transfer proves the bytes already
// moved are the right ones. Each record therefore carries a fingerprint of
// whichever side is the source of truth for the content — the local file for an
// upload, the remote object for a download — and a resume is refused when that
// fingerprint no longer matches.
//
// Getting this wrong is silent. On upload the server seeks to whatever offset it
// is given without checking the partial file is really that long, so a stale
// offset writes new bytes into the middle of old ones. On download an unverified
// partial gets the remote tail appended to it. Neither produces an error, a bad
// status, or a size mismatch — only a corrupt file.
//
// The upload offset additionally has nowhere else to live: no endpoint reports how
// much of a partial upload arrived, and a stat of the destination 404s until the
// final chunk lands.
package resume

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Kind distinguishes the two directions, so an upload record can never satisfy a
// download lookup for the same remote path.
type Kind string

const (
	Upload   Kind = "upload"
	Download Kind = "download"
)

// Record is one interrupted transfer.
type Record struct {
	Kind       Kind   `json:"kind"`
	Source     string `json:"source"`
	RemotePath string `json:"remotePath"`
	// LocalPath distinguishes two downloads of the same remote file to different
	// destinations. Empty for an upload, where the remote path is the destination.
	LocalPath string `json:"localPath,omitempty"`

	Offset    int64 `json:"offset"`
	TotalSize int64 `json:"totalSize"`

	// FingerprintSize and FingerprintMod describe the side that must not have
	// changed: the local file for an upload, the remote object for a download.
	FingerprintSize int64     `json:"fingerprintSize"`
	FingerprintMod  time.Time `json:"fingerprintModified"`

	UpdatedAt time.Time `json:"updatedAt"`
}

func (r Record) key() string {
	return string(r.Kind) + "\x00" + r.Source + "\x00" + r.RemotePath + "\x00" + r.LocalPath
}

// Store persists records as JSON under the XDG state directory. State, not cache:
// losing it silently restarts a multi-gigabyte transfer from zero.
type Store struct {
	path string
}

// DefaultPath reports where the resume state lives, honoring XDG_STATE_HOME.
func DefaultPath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "ifiles", "transfers.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ifiles-transfers.json")
	}
	return filepath.Join(home, ".local", "state", "ifiles", "transfers.json")
}

func NewStore() *Store { return &Store{path: DefaultPath()} }

// NewStoreAt builds a store over a specific file, for tests.
func NewStoreAt(path string) *Store { return &Store{path: path} }

func (s *Store) Path() string { return s.path }

// Lookup returns the offset to resume from, and whether resuming is safe.
//
// query identifies the transfer and carries the *current* fingerprint of its
// content source. A resume is only offered when that matches what was recorded,
// and when observed — the length of the partial data as the caller sees it now —
// agrees with the recorded offset. For a download that length is the partial
// file's size; for an upload the caller cannot observe it, and passes the recorded
// offset back by supplying -1.
func (s *Store) Lookup(query Record, observed int64) (int64, bool) {
	records, err := s.load()
	if err != nil {
		return 0, false
	}
	record, ok := records[query.key()]
	if !ok {
		return 0, false
	}

	if record.FingerprintSize != query.FingerprintSize {
		return 0, false
	}
	if !record.FingerprintMod.Equal(query.FingerprintMod) {
		return 0, false
	}
	if record.TotalSize != query.TotalSize {
		return 0, false
	}
	if record.Offset <= 0 || record.Offset >= record.TotalSize {
		return 0, false
	}
	// A partial whose length has drifted from the record was touched by something
	// else, and its contents can no longer be vouched for.
	if observed >= 0 && observed != record.Offset {
		return 0, false
	}
	return record.Offset, true
}

// Save records progress, replacing any earlier record for the same transfer.
func (s *Store) Save(record Record) error {
	records, err := s.load()
	if err != nil {
		return err
	}
	record.UpdatedAt = time.Now().UTC()
	records[record.key()] = record
	return s.write(records)
}

// Clear forgets a transfer, called on completion so a finished one cannot be
// mistaken for a resumable one.
func (s *Store) Clear(record Record) error {
	records, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := records[record.key()]; !ok {
		return nil
	}
	delete(records, record.key())
	return s.write(records)
}

// List returns every recorded interrupted transfer.
func (s *Store) List() ([]Record, error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	list := make([]Record, 0, len(records))
	for _, record := range records {
		list = append(list, record)
	}
	return list, nil
}

func (s *Store) load() (map[string]Record, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}

	records := map[string]Record{}
	if err := json.Unmarshal(data, &records); err != nil {
		// A corrupt state file must not block a transfer. Starting over costs
		// bandwidth; refusing to run costs the whole command.
		return map[string]Record{}, nil
	}
	return records, nil
}

// write replaces the state file atomically, so an interrupt during the write
// cannot leave behind a truncated file whose offsets would corrupt a later resume.
func (s *Store) write(records map[string]Record) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(s.path), err)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(s.path), ".transfers-*.json")
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
	if err := os.Chmod(tempName, 0o600); err != nil {
		return err
	}
	return os.Rename(tempName, s.path)
}
