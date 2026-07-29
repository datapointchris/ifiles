package resume

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStoreAt(filepath.Join(t.TempDir(), "transfers.json"))
}

// uploadRecord is the shape `put` builds: fingerprinted by the local file.
func uploadRecord(modified time.Time) Record {
	return Record{
		Kind:            Upload,
		Source:          "files",
		RemotePath:      "/media/video.mkv",
		TotalSize:       512 << 20,
		FingerprintSize: 512 << 20,
		FingerprintMod:  modified,
	}
}

// downloadRecord is the shape `get` builds: fingerprinted by the remote object.
func downloadRecord(modified time.Time) Record {
	return Record{
		Kind:            Download,
		Source:          "files",
		RemotePath:      "/media/video.mkv",
		LocalPath:       "video.mkv",
		TotalSize:       1000,
		FingerprintSize: 1000,
		FingerprintMod:  modified,
	}
}

func TestUploadRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)

	saved := uploadRecord(modified)
	saved.Offset = 64 << 20
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// -1: an upload's progress cannot be observed from the client side.
	offset, ok := store.Lookup(uploadRecord(modified), -1)
	if !ok {
		t.Fatal("Lookup() found nothing after Save()")
	}
	if offset != 64<<20 {
		t.Errorf("offset = %d, want %d", offset, 64<<20)
	}
}

func TestUploadLookupRejectsAChangedLocalFile(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	saved := uploadRecord(modified)
	saved.Offset = 1000
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// The server seeks to whatever offset it is given without checking the temp
	// file is really that long, so resuming a file whose contents changed writes
	// new bytes into the middle of an old upload and produces a corrupt result
	// that passes every check the server makes.
	resized := uploadRecord(modified)
	resized.FingerprintSize = 400 << 20
	resized.TotalSize = 400 << 20
	if _, ok := store.Lookup(resized, -1); ok {
		t.Error("Lookup() allowed a resume after the local size changed")
	}

	touched := uploadRecord(modified.Add(time.Second))
	if _, ok := store.Lookup(touched, -1); ok {
		t.Error("Lookup() allowed a resume after the local modification time changed")
	}
}

func TestDownloadLookupRejectsAChangedRemoteFile(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	saved := downloadRecord(modified)
	saved.Offset = 400
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A remote file replaced between attempts would otherwise have its new tail
	// appended to the old head: right size, wrong contents, no error.
	if _, ok := store.Lookup(downloadRecord(modified.Add(time.Hour)), 400); ok {
		t.Error("Lookup() allowed a resume after the remote file was modified")
	}
}

func TestDownloadLookupRejectsAPartialOfTheWrongLength(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()
	saved := downloadRecord(modified)
	saved.Offset = 400
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// A partial file whose length has drifted from the record was touched by
	// something else, so its contents can no longer be vouched for. This is what
	// makes a leftover ".ifilespart" from an unrelated download safe.
	if _, ok := store.Lookup(downloadRecord(modified), 50); ok {
		t.Error("Lookup() resumed against a partial file of the wrong length")
	}
	if _, ok := store.Lookup(downloadRecord(modified), 400); !ok {
		t.Error("Lookup() refused a partial file matching the record exactly")
	}
}

func TestDownloadWithNoRecordDoesNotResume(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	// A partial file with no record behind it — written by an older version, or by
	// something else entirely — has unknown provenance and must start over.
	if _, ok := store.Lookup(downloadRecord(time.Now().UTC()), 500); ok {
		t.Error("Lookup() resumed a partial file it had never recorded")
	}
}

func TestUploadAndDownloadRecordsDoNotCollide(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()

	up := uploadRecord(modified)
	up.Offset = 100
	if err := store.Save(up); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// Same source and remote path, opposite direction. An offset means a different
	// thing in each, so one must never satisfy the other's lookup.
	if _, ok := store.Lookup(downloadRecord(modified), 100); ok {
		t.Error("an upload record satisfied a download lookup for the same path")
	}
}

func TestLookupRejectsAnOffsetAtOrPastTheEnd(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()

	for _, offset := range []int64{0, -5, 1000, 9000} {
		saved := downloadRecord(modified)
		saved.Offset = offset
		if err := store.Save(saved); err != nil {
			t.Fatalf("Save() returned error: %v", err)
		}
		// A complete transfer is not a resumable one, and an offset past the end
		// could only corrupt.
		if _, ok := store.Lookup(downloadRecord(modified), offset); ok {
			t.Errorf("Lookup() offered a resume at offset %d of a 1000 byte file", offset)
		}
	}
}

func TestLookupIsScopedPerSourceAndPath(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()
	saved := uploadRecord(modified)
	saved.Offset = 100
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	otherSource := uploadRecord(modified)
	otherSource.Source = "archive"
	if _, ok := store.Lookup(otherSource, -1); ok {
		t.Error("a record for one source resumed an upload to another")
	}

	otherPath := uploadRecord(modified)
	otherPath.RemotePath = "/media/other.mkv"
	if _, ok := store.Lookup(otherPath, -1); ok {
		t.Error("a record for one path resumed an upload to another")
	}
}

func TestDownloadIsScopedPerLocalTarget(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()
	saved := downloadRecord(modified)
	saved.Offset = 400
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// The same remote file downloaded to two names has two independent partials.
	elsewhere := downloadRecord(modified)
	elsewhere.LocalPath = "/other/video.mkv"
	if _, ok := store.Lookup(elsewhere, 400); ok {
		t.Error("a record for one destination resumed a download to another")
	}
}

func TestClearForgetsACompletedTransfer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()
	saved := uploadRecord(modified)
	saved.Offset = 100
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if err := store.Clear(saved); err != nil {
		t.Fatalf("Clear() returned error: %v", err)
	}

	// A finished transfer left in the store would make the next one resume from a
	// stale offset instead of starting fresh.
	if _, ok := store.Lookup(uploadRecord(modified), -1); ok {
		t.Error("Lookup() still found a cleared record")
	}
	if err := store.Clear(saved); err != nil {
		t.Errorf("clearing an absent record returned error: %v", err)
	}
}

func TestSaveReplacesAnEarlierRecord(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	modified := time.Now().UTC()

	saved := uploadRecord(modified)
	saved.Offset = 100
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	saved.Offset = 300
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	offset, ok := store.Lookup(uploadRecord(modified), -1)
	if !ok {
		t.Fatal("Lookup() found nothing")
	}
	if offset != 300 {
		t.Errorf("offset = %d, want the most recent 300", offset)
	}
	records, err := store.List()
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("List() returned %d records, want 1", len(records))
	}
}

func TestCorruptStateDoesNotBlockATransfer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "transfers.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}
	store := NewStoreAt(path)

	// Starting over costs bandwidth. Refusing to run costs the whole command, so a
	// corrupt state file degrades to "no resume available".
	if _, ok := store.Lookup(uploadRecord(time.Now().UTC()), -1); ok {
		t.Error("Lookup() returned an offset from a corrupt state file")
	}
	saved := uploadRecord(time.Now().UTC())
	saved.Offset = 10
	if err := store.Save(saved); err != nil {
		t.Errorf("Save() over a corrupt state file returned error: %v", err)
	}
}

func TestSaveWritesOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	saved := uploadRecord(time.Now().UTC())
	saved.Offset = 1
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %v, want 0600", perm)
	}
}

func TestSaveLeavesNoTempFilesBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "transfers.json"))
	saved := uploadRecord(time.Now().UTC())
	for i := range 5 {
		saved.Offset = int64(i+1) * 10
		if err := store.Save(saved); err != nil {
			t.Fatalf("Save() returned error: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() returned error: %v", err)
	}
	// The write is a temp file plus a rename, so a leaked temp on every progress
	// tick would litter the state directory during a long transfer.
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("state directory holds %v, want only transfers.json", names)
	}
}

func TestDefaultPathHonorsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")

	want := "/custom/state/ifiles/transfers.json"
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
