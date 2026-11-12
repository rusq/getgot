package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// stateDir returns the per-user temporary directory.
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
