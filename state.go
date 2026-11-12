package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const pidName = "getgot.pid"

// pidPath returns the path of the PID file in the private, per-user state
// directory.  It creates the directory if needed.
func pidPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", fmt.Errorf("state directory: %w", err)
	}
	return filepath.Join(dir, pidName), nil
}

// privateDir creates dir with mode 0700 if it does not exist, and verifies
// that it is a real directory, not a symlink, owned by the current user and
// not accessible to anyone else.
func privateDir(dir string) (string, error) {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if err := checkPrivate(fi); err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	return dir, nil
}
