package main

import (
	"path/filepath"
	"testing"
)

func TestPIDPath(t *testing.T) {
	t.Run("is absolute and stable", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
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
