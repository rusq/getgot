//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestStateDir(t *testing.T) {
	tmpName := "getgot-" + strconv.Itoa(os.Getuid())
	tests := []struct {
		name    string
		xdg     string                          // XDG_RUNTIME_DIR, "temp" for a temp directory
		prepare func(t *testing.T, want string) // runs before stateDir
		wantErr bool
	}{
		{name: "xdg runtime dir", xdg: "temp"},
		{name: "relative xdg runtime dir is ignored", xdg: "run/user"},
		{name: "temp dir fallback"},
		{
			name: "existing private dir is reused",
			prepare: func(t *testing.T, want string) {
				if err := os.Mkdir(want, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "group readable dir is rejected",
			prepare: func(t *testing.T, want string) {
				if err := os.Mkdir(want, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(want, 0o750); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: true,
		},
		{
			name: "symlink is rejected",
			prepare: func(t *testing.T, want string) {
				target := t.TempDir()
				if err := os.Symlink(target, want); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: true,
		},
		{
			name: "regular file is rejected",
			prepare: func(t *testing.T, want string) {
				if err := os.WriteFile(want, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			want := filepath.Join(tmp, tmpName)
			switch tt.xdg {
			case "temp":
				xdg := t.TempDir()
				t.Setenv("XDG_RUNTIME_DIR", xdg)
				want = filepath.Join(xdg, "getgot")
			default:
				t.Setenv("XDG_RUNTIME_DIR", tt.xdg)
			}
			if tt.prepare != nil {
				tt.prepare(t, want)
			}
			got, err := stateDir()
			if tt.wantErr {
				if !errors.Is(err, errNotPrivate) {
					t.Fatalf("stateDir() error = %v, want %v", err, errNotPrivate)
				}
				return
			}
			if err != nil {
				t.Fatalf("stateDir() error = %v", err)
			}
			if got != want {
				t.Errorf("stateDir() = %q, want %q", got, want)
			}
			fi, err := os.Lstat(got)
			if err != nil {
				t.Fatal(err)
			}
			if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
				t.Errorf("stateDir() created %v, want a 0700 directory", fi.Mode())
			}
		})
	}
}

// fakeInfo is a directory FileInfo with a chosen owner and mode.
type fakeInfo struct {
	mode fs.FileMode
	uid  uint32
}

func (f fakeInfo) Name() string       { return "dir" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }

func TestStateDirUmask(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("XDG_RUNTIME_DIR", "")
	old := syscall.Umask(0o277)
	defer syscall.Umask(old)
	got, err := stateDir()
	if err != nil {
		t.Fatalf("stateDir() with umask 0277: %v", err)
	}
	fi, err := os.Lstat(got)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %#o, want 0700", fi.Mode().Perm())
	}
}

func TestCheckPrivate(t *testing.T) {
	me := uint32(os.Getuid())
	tests := []struct {
		name    string
		fi      fs.FileInfo
		wantErr bool
	}{
		{"private dir", fakeInfo{fs.ModeDir | 0o700, me}, false},
		{"other owner", fakeInfo{fs.ModeDir | 0o700, me + 1}, true},
		{"world writable", fakeInfo{fs.ModeDir | 0o777, me}, true},
		{"symlink", fakeInfo{fs.ModeSymlink | 0o700, me}, true},
		{"file", fakeInfo{0o700, me}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPrivate(tt.fi)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPrivate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
