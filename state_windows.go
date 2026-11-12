package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// stateDir returns getgot's directory in the temporary directory, which is
// per-user by default (%TEMP% under the user profile).
func stateDir() (string, error) {
	return privateDir(filepath.Join(os.TempDir(), "getgot"))
}

// checkPrivate only checks that fi describes a directory; Windows access
// control lists are not inspected.
func checkPrivate(fi fs.FileInfo) error {
	if !fi.IsDir() {
		return fs.ErrInvalid
	}
	return nil
}
