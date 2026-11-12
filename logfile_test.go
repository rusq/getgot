package main

import (
	"bytes"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLogPath(t *testing.T) {
	cache := t.TempDir()
	// os.UserCacheDir: XDG_CACHE_HOME on Linux, $HOME/Library/Caches on
	// macOS, %LocalAppData% on Windows.
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("LocalAppData", cache)
	wd := t.TempDir()
	t.Chdir(wd)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "relative is made absolute", in: "x.log", want: filepath.Join(wd, "x.log")},
		{name: "absolute is kept", in: filepath.Join(cache, "y.log"), want: filepath.Join(cache, "y.log")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := logPath(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("logPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	t.Run("default is in the cache directory", func(t *testing.T) {
		got, err := logPath("")
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(got) || filepath.Base(got) != logFileName || filepath.Base(filepath.Dir(got)) != "getgot" {
			t.Errorf("logPath(\"\") = %q, want <cache>/getgot/%s", got, logFileName)
		}
		if _, err := os.Stat(filepath.Dir(got)); err != nil {
			t.Errorf("log directory not created: %v", err)
		}
	})
}

func TestOpenLog(t *testing.T) {
	t.Run("creates with mode 0600 and appends", func(t *testing.T) {
		name := filepath.Join(t.TempDir(), "a.log")
		for _, line := range []string{"one\n", "two\n"} {
			f, err := openLog(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(line); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "one\ntwo\n" {
			t.Errorf("content = %q, want appended lines", b)
		}
		if fi, _ := os.Stat(name); fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
			t.Errorf("mode = %#o, want 0600", fi.Mode().Perm())
		}
	})
	t.Run("missing directory fails", func(t *testing.T) {
		if _, err := openLog(filepath.Join(t.TempDir(), "no", "such", "a.log")); err == nil {
			t.Error("openLog succeeded, want an error")
		}
	})
}

func TestSetLogOutput(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	var buf bytes.Buffer
	setLogOutput(&buf)
	slog.Info("from slog", "pid", 42)
	log.Print("from log")

	out := buf.String()
	for _, want := range []string{"from slog", "pid=42", "from log"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output %q does not contain %q", out, want)
		}
	}
}
