//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

var errNotPrivate = errors.New("not a private directory")

// stateDir returns a per-user directory on a local filesystem, suitable for
// the PID and lock files: $XDG_RUNTIME_DIR/getgot when XDG_RUNTIME_DIR is set,
// or <tmp>/getgot-<uid> otherwise.  Home directories are avoided because they
// may be on a network filesystem, where fcntl lock holders are meaningless.
func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(xdg) {
		return privateDir(filepath.Join(xdg, "getgot"))
	}
	return privateDir(filepath.Join(os.TempDir(), "getgot-"+strconv.Itoa(os.Getuid())))
}

// checkPrivate returns an error unless fi describes a directory, not a
// symlink, owned by the current user with permissions 0700.
func checkPrivate(fi fs.FileInfo) error {
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("%w: not a directory", errNotPrivate)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: unknown owner", errNotPrivate)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: owned by uid %d", errNotPrivate, st.Uid)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		return fmt.Errorf("%w: mode %#o, want 0700", errNotPrivate, perm)
	}
	return nil
}
