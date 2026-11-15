// Package atomicfile replaces a file ifiles owns in one step, owner-only.
//
// Every file ifiles writes for itself is read back by a later run: the fallback
// token, the resume offsets and the completion cache. A reader must see either
// the old contents or the new ones, never half a file, so the bytes go to a temp
// file in the target's own directory and are renamed over the target. A rename
// within one directory is atomic on the filesystems ifiles runs on.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data, creating its directory 0700 if needed.
//
// tempPattern names the temp file, as os.CreateTemp takes it, so a stray one left
// by a killed process says which file it belonged to. The temp file is removed on
// every path that does not rename it.
//
// os.CreateTemp already creates the file 0600, but the mode is set explicitly as
// well: this is the line that keeps a token from being world readable, and it
// should not survive only as a property of the standard library.
func Write(path, tempPattern string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, tempPattern)
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
	return os.Rename(tempName, path)
}
