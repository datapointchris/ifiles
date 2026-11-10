package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalTargetDefaultsToTheRemoteBasename(t *testing.T) {
	t.Parallel()

	got, err := localTarget("/photos/sunset.jpg", "", []string{"/photos/sunset.jpg"})
	if err != nil {
		t.Fatalf("localTarget() returned error: %v", err)
	}
	if got != "sunset.jpg" {
		t.Errorf("localTarget() = %q, want sunset.jpg", got)
	}
}

func TestLocalTargetAppendsTheArchiveExtension(t *testing.T) {
	t.Parallel()

	got, err := localTarget("/photos", "tar.gz", []string{"/photos"})
	if err != nil {
		t.Fatalf("localTarget() returned error: %v", err)
	}
	// Without the extension the file is a tarball named like a directory, and
	// nothing downstream can tell what it is.
	if got != "photos.tar.gz" {
		t.Errorf("localTarget() = %q, want photos.tar.gz", got)
	}
}

func TestLocalTargetGoesInsideAnExistingDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := localTarget("/photos/sunset.jpg", "", []string{"/photos/sunset.jpg", dir})
	if err != nil {
		t.Fatalf("localTarget() returned error: %v", err)
	}
	// Matching cp and scp: a destination that is a directory means "into it",
	// not "overwrite it with a file of that name".
	if want := filepath.Join(dir, "sunset.jpg"); got != want {
		t.Errorf("localTarget() = %q, want %q", got, want)
	}
}

func TestLocalTargetTreatsANonDirectoryAsTheFilename(t *testing.T) {
	t.Parallel()

	got, err := localTarget("/photos/sunset.jpg", "", []string{"/photos/sunset.jpg", "renamed.jpg"})
	if err != nil {
		t.Fatalf("localTarget() returned error: %v", err)
	}
	if got != "renamed.jpg" {
		t.Errorf("localTarget() = %q, want renamed.jpg", got)
	}
}

func TestLocalTargetRejectsAMissingDirectory(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent") + string(os.PathSeparator)
	// A trailing separator says the caller meant a directory. Silently creating a
	// file with that name would be surprising.
	if _, err := localTarget("/photos/sunset.jpg", "", []string{"/photos/sunset.jpg", missing}); err == nil {
		t.Error("localTarget() accepted a trailing-slash path that does not exist")
	}
}
