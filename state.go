package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
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
	if err := os.Mkdir(dir, 0o700); err == nil {
		// the umask may have cleared owner bits.
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", fmt.Errorf("chmod %s: %w", dir, err)
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := checkPrivate(fi); err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	return dir, nil
}

// stateFiles returns the files the daemon keeps in the state directory: the
// directory itself, the PID file and gotsr's lock file next to it.
func stateFiles(pidFile string) []string {
	return []string{filepath.Dir(pidFile), pidFile, pidFile + ".lock"}
}

// touch updates the access and modification times of the files, so that
// temporary directory cleaners (macOS dirhelper, systemd-tmpfiles) do not
// remove the state of a long running daemon.  Removing the lock file would
// let a second daemon start.  Chtimes does not open the files, so it does
// not disturb the daemon's fcntl lock.
func touch(files ...string) {
	now := time.Now()
	for _, f := range files {
		if err := os.Chtimes(f, now, now); err != nil {
			slog.Warn("touch state file", "file", f, "error", err)
		}
	}
}
