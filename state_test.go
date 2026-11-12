package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPIDPath(t *testing.T) {
	t.Run("is absolute and stable", func(t *testing.T) {
		tmp := t.TempDir()
		for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
			t.Setenv(v, tmp)
		}
		t.Setenv("XDG_RUNTIME_DIR", "")
		a, err := pidPath()
		if err != nil {
			t.Fatal(err)
		}
		t.Chdir(t.TempDir())
		b, err := pidPath()
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(a) || a != b {
			t.Errorf("pidPath() = %q, then %q after chdir; want the same absolute path", a, b)
		}
		if filepath.Base(a) != pidName {
			t.Errorf("pidPath() = %q, want base %q", a, pidName)
		}
	})
}

func TestTouch(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, pidName)
	for _, f := range stateFiles(pidFile)[1:] {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-72 * time.Hour)
	for _, f := range stateFiles(pidFile) {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	touch(stateFiles(pidFile)...)
	for _, f := range stateFiles(pidFile) {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(fi.ModTime()) > time.Hour {
			t.Errorf("%s: mtime %v not updated", f, fi.ModTime())
		}
	}
}
