package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mitchellh/go-ps"
)

func TestNewFileLoggerDoesNotInstallDefaults(t *testing.T) {
	oldSlog := slog.Default()
	oldLogWriter := log.Writer()
	t.Cleanup(func() {
		slog.SetDefault(oldSlog)
		log.SetOutput(oldLogWriter)
	})

	var structuredTerminal, legacyTerminal bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&structuredTerminal, nil)))
	log.SetOutput(&legacyTerminal)

	filename := filepath.Join(t.TempDir(), defLogName)
	f, daemonLogger, err := newFileLogger(filename, true)
	if err != nil {
		t.Fatalf("newFileLogger() unexpected error: %v", err)
	}
	defer f.Close()

	slog.Info("foreground structured")
	log.Print("foreground legacy")
	if err := f.Sync(); err != nil {
		t.Fatalf("sync daemon log: %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("daemon log contains foreground output before installation: %q", data)
	}
	if !strings.Contains(structuredTerminal.String(), "foreground structured") {
		t.Errorf("structured foreground output = %q", structuredTerminal.String())
	}
	if !strings.Contains(legacyTerminal.String(), "foreground legacy") {
		t.Errorf("legacy foreground output = %q", legacyTerminal.String())
	}

	daemonLogger.Info("dedicated daemon event")
	if err := f.Sync(); err != nil {
		t.Fatalf("sync dedicated daemon event: %v", err)
	}
	data, err = os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(data), "dedicated daemon event") {
		t.Errorf("daemon log = %q, want dedicated event", data)
	}
}

func TestDaemonLifecycleLogging(t *testing.T) {
	filename := filepath.Join(t.TempDir(), defLogName)
	f, logger, err := newFileLogger(filename, true)
	if err != nil {
		t.Fatalf("newFileLogger() unexpected error: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	if err := logLifecycleEvent(logger, startupLogEvent, f); err != nil {
		t.Fatalf("log startup event: %v", err)
	}
	daemonAtExit(cancel, logger, f)()

	select {
	case <-ctx.Done():
	default:
		t.Fatal("daemonAtExit() did not cancel the daemon context")
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read lifecycle log: %v", err)
	}
	contents := string(data)
	for _, event := range []string{startupLogEvent, shutdownLogEvent} {
		if count := strings.Count(contents, `msg="`+event+`"`); count != 1 {
			t.Errorf("lifecycle log contains %q %d times, want exactly once: %q", event, count, contents)
		}
	}
}

func TestDaemonAtExitCancelsAfterLogSyncFailure(t *testing.T) {
	filename := filepath.Join(t.TempDir(), defLogName)
	f, logger, err := newFileLogger(filename, true)
	if err != nil {
		t.Fatalf("newFileLogger() unexpected error: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close log file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	daemonAtExit(cancel, logger, f)()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("daemonAtExit() did not cancel after the log sync failed")
	}
}

func TestSelectLogFile(t *testing.T) {
	stateDir := t.TempDir()

	tests := []struct {
		name        string
		configured  string
		want        string
		wantPrivate bool
	}{
		{
			name:        "default",
			configured:  defLogName,
			want:        filepath.Join(stateDir, defLogName),
			wantPrivate: true,
		},
		{
			name:        "empty uses default",
			configured:  "",
			want:        filepath.Join(stateDir, defLogName),
			wantPrivate: true,
		},
		{
			name:       "explicit relative path",
			configured: filepath.Join("logs", "custom.log"),
			want:       filepath.Join("logs", "custom.log"),
		},
		{
			name:       "explicit absolute path",
			configured: filepath.Join(stateDir, "custom.log"),
			want:       filepath.Join(stateDir, "custom.log"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, private := selectLogFile(tt.configured, stateDir)
			if got != tt.want || private != tt.wantPrivate {
				t.Fatalf("selectLogFile(%q, %q) = %q, %t, want %q, %t", tt.configured, stateDir, got, private, tt.want, tt.wantPrivate)
			}
			if tt.wantPrivate && !filepath.IsAbs(got) {
				t.Errorf("selectLogFile() default = %q, want an absolute path", got)
			}
		})
	}
}

func TestFileLogging(t *testing.T) {
	oldSlog := slog.Default()
	oldLogWriter := log.Writer()
	t.Cleanup(func() {
		slog.SetDefault(oldSlog)
		log.SetOutput(oldLogWriter)
	})

	t.Run("routes structured and legacy messages", func(t *testing.T) {
		filename := filepath.Join(t.TempDir(), defLogName)
		f, logger, err := newFileLogger(filename, true)
		if err != nil {
			t.Fatalf("newFileLogger() unexpected error: %v", err)
		}
		defer f.Close()
		installDefaultLogging(logger, f)

		slog.Info("structured message", "answer", 42)
		log.Print("legacy message")
		if err := f.Sync(); err != nil {
			t.Fatalf("sync log file: %v", err)
		}

		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("read log file: %v", err)
		}
		contents := string(data)
		for _, want := range []string{"structured message", "answer=42", "legacy message"} {
			if !strings.Contains(contents, want) {
				t.Errorf("log contents = %q, want %q", contents, want)
			}
		}
	})

	t.Run("creates private file", func(t *testing.T) {
		filename := filepath.Join(t.TempDir(), defLogName)
		f, _, err := newFileLogger(filename, true)
		if err != nil {
			t.Fatalf("newFileLogger() unexpected error: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close log file: %v", err)
		}
		if runtime.GOOS == "windows" {
			return
		}
		info, err := os.Stat(filename)
		if err != nil {
			t.Fatalf("stat log file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("log file permissions = %04o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("hardens existing default file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not implement Unix permission bits")
		}
		filename := filepath.Join(t.TempDir(), defLogName)
		if err := os.WriteFile(filename, nil, 0o644); err != nil {
			t.Fatalf("create permissive log file: %v", err)
		}
		if err := os.Chmod(filename, 0o644); err != nil {
			t.Fatalf("set permissive log file mode: %v", err)
		}

		f, _, err := newFileLogger(filename, true)
		if err != nil {
			t.Fatalf("newFileLogger() unexpected error: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close log file: %v", err)
		}
		info, err := os.Stat(filename)
		if err != nil {
			t.Fatalf("stat log file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("log file permissions = %04o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("preserves existing explicit file permissions", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not implement Unix permission bits")
		}
		filename := filepath.Join(t.TempDir(), "custom.log")
		if err := os.WriteFile(filename, nil, 0o640); err != nil {
			t.Fatalf("create custom log file: %v", err)
		}
		if err := os.Chmod(filename, 0o640); err != nil {
			t.Fatalf("set custom log file mode: %v", err)
		}

		f, _, err := newFileLogger(filename, false)
		if err != nil {
			t.Fatalf("newFileLogger() unexpected error: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close log file: %v", err)
		}
		info, err := os.Stat(filename)
		if err != nil {
			t.Fatalf("stat log file: %v", err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Errorf("explicit log file permissions = %04o, want 0640", info.Mode().Perm())
		}
	})
}

func TestStatePIDFile(t *testing.T) {
	t.Run("stable absolute path across working directories", func(t *testing.T) {
		baseDir := t.TempDir()
		lookup := func() (string, error) { return baseDir, nil }

		firstWorkingDir := t.TempDir()
		secondWorkingDir := t.TempDir()
		t.Chdir(firstWorkingDir)
		first, err := statePIDFile(lookup)
		if err != nil {
			t.Fatalf("statePIDFile() first call: %v", err)
		}
		t.Chdir(secondWorkingDir)
		second, err := statePIDFile(lookup)
		if err != nil {
			t.Fatalf("statePIDFile() second call: %v", err)
		}

		want := filepath.Join(baseDir, stateDirName, pidFileName)
		if first != want || second != want {
			t.Fatalf("statePIDFile() paths = %q, %q, want %q", first, second, want)
		}
		if !filepath.IsAbs(first) {
			t.Errorf("statePIDFile() = %q, want an absolute path", first)
		}
	})

	t.Run("creates private state directory", func(t *testing.T) {
		baseDir := t.TempDir()
		got, err := statePIDFile(func() (string, error) { return baseDir, nil })
		if err != nil {
			t.Fatalf("statePIDFile() unexpected error: %v", err)
		}
		stateDir := filepath.Dir(got)
		info, err := os.Stat(stateDir)
		if err != nil {
			t.Fatalf("stat state directory: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("state path %q is not a directory", stateDir)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Errorf("state directory permissions = %04o, want 0700", info.Mode().Perm())
		}
	})

	t.Run("hardens existing state directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not implement Unix permission bits")
		}
		baseDir := t.TempDir()
		stateDir := filepath.Join(baseDir, stateDirName)
		if err := os.Mkdir(stateDir, 0o755); err != nil {
			t.Fatalf("create permissive state directory: %v", err)
		}
		if err := os.Chmod(stateDir, 0o755); err != nil {
			t.Fatalf("set permissive state directory mode: %v", err)
		}

		if _, err := statePIDFile(func() (string, error) { return baseDir, nil }); err != nil {
			t.Fatalf("statePIDFile() unexpected error: %v", err)
		}
		info, err := os.Stat(stateDir)
		if err != nil {
			t.Fatalf("stat state directory: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("state directory permissions = %04o, want 0700", info.Mode().Perm())
		}
	})

	t.Run("returns config lookup error", func(t *testing.T) {
		lookupErr := errors.New("lookup failed")
		_, err := statePIDFile(func() (string, error) { return "", lookupErr })
		if !errors.Is(err, lookupErr) {
			t.Fatalf("statePIDFile() error = %v, want errors.Is(_, %v)", err, lookupErr)
		}
	})

	t.Run("rejects relative config directory", func(t *testing.T) {
		_, err := statePIDFile(func() (string, error) { return "relative", nil })
		if err == nil || !strings.Contains(err.Error(), "not absolute") {
			t.Fatalf("statePIDFile() error = %v, want relative-path error", err)
		}
	})
}

func TestParseProcessNames(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr string
	}{
		{
			name:  "default list",
			value: "jamf,Nudge,du",
			want:  []string{"jamf", "Nudge", "du"},
		},
		{
			name:    "empty",
			value:   "",
			wantErr: "process name 1 is empty",
		},
		{
			name:    "whitespace only",
			value:   " \t\n",
			wantErr: "process name 1 is empty",
		},
		{
			name:  "trims whitespace",
			value: "  jamf, Nudge ,\tdu\n",
			want:  []string{"jamf", "Nudge", "du"},
		},
		{
			name:  "deduplicates while preserving order",
			value: "du,jamf,du,Nudge,jamf",
			want:  []string{"du", "jamf", "Nudge"},
		},
		{
			name:    "mixed valid and empty",
			value:   "jamf, ,du",
			wantErr: "process name 2 is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcessNames(tt.value)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseProcessNames(%q) error = nil, want %q", tt.value, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseProcessNames(%q) error = %q, want it to contain %q", tt.value, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcessNames(%q) unexpected error: %v", tt.value, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseProcessNames(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

type fakeProcess struct {
	pid        int
	ppid       int
	executable string
}

func (p fakeProcess) Pid() int           { return p.pid }
func (p fakeProcess) PPid() int          { return p.ppid }
func (p fakeProcess) Executable() string { return p.executable }

type fakeKiller struct {
	pid     int
	killErr error
	killLog *[]int
}

func (p fakeKiller) Kill() error {
	*p.killLog = append(*p.killLog, p.pid)
	return p.killErr
}

func TestWipeProcesses(t *testing.T) {
	t.Run("kills every matching process", func(t *testing.T) {
		processes := []ps.Process{
			fakeProcess{pid: 10, executable: "target"},
			fakeProcess{pid: 11, executable: "other"},
			fakeProcess{pid: 12, executable: "target"},
		}
		var findLog, killLog []int
		err := wipeProcesses(map[string]bool{"target": true}, func() ([]ps.Process, error) {
			return processes, nil
		}, func(pid int) (processKiller, error) {
			findLog = append(findLog, pid)
			return fakeKiller{pid: pid, killLog: &killLog}, nil
		})
		if err != nil {
			t.Fatalf("wipeProcesses() unexpected error: %v", err)
		}
		if want := []int{10, 12}; !reflect.DeepEqual(findLog, want) {
			t.Errorf("find order = %v, want %v", findLog, want)
		}
		if want := []int{10, 12}; !reflect.DeepEqual(killLog, want) {
			t.Errorf("kill order = %v, want %v", killLog, want)
		}
	})

	t.Run("continues after find and kill failures", func(t *testing.T) {
		findErr := errors.New("find failed")
		firstKillErr := errors.New("first kill failed")
		secondKillErr := errors.New("second kill failed")
		processes := []ps.Process{
			fakeProcess{pid: 20, executable: "target"},
			fakeProcess{pid: 21, executable: "target"},
			fakeProcess{pid: 22, executable: "target"},
			fakeProcess{pid: 23, executable: "other"},
			fakeProcess{pid: 24, executable: "target"},
		}
		var findLog, killLog []int
		err := wipeProcesses(map[string]bool{"target": true}, func() ([]ps.Process, error) {
			return processes, nil
		}, func(pid int) (processKiller, error) {
			findLog = append(findLog, pid)
			switch pid {
			case 20:
				return nil, findErr
			case 21:
				return fakeKiller{pid: pid, killErr: firstKillErr, killLog: &killLog}, nil
			case 24:
				return fakeKiller{pid: pid, killErr: secondKillErr, killLog: &killLog}, nil
			default:
				return fakeKiller{pid: pid, killLog: &killLog}, nil
			}
		})
		for _, wantErr := range []error{findErr, firstKillErr, secondKillErr} {
			if !errors.Is(err, wantErr) {
				t.Errorf("wipeProcesses() error = %v, want errors.Is(_, %v)", err, wantErr)
			}
		}
		if want := []int{20, 21, 22, 24}; !reflect.DeepEqual(findLog, want) {
			t.Errorf("find order = %v, want %v", findLog, want)
		}
		if want := []int{21, 22, 24}; !reflect.DeepEqual(killLog, want) {
			t.Errorf("kill order = %v, want %v", killLog, want)
		}
		for _, text := range []string{"unable to find process 20", "failed to kill target (PID 21)", "failed to kill target (PID 24)"} {
			if !strings.Contains(err.Error(), text) {
				t.Errorf("wipeProcesses() error = %q, want it to contain %q", err, text)
			}
		}
	})

	t.Run("returns process listing failure", func(t *testing.T) {
		listErr := errors.New("list failed")
		findCalled := false
		err := wipeProcesses(map[string]bool{"target": true}, func() ([]ps.Process, error) {
			return nil, listErr
		}, func(int) (processKiller, error) {
			findCalled = true
			return nil, nil
		})
		if !errors.Is(err, listErr) {
			t.Fatalf("wipeProcesses() error = %v, want errors.Is(_, %v)", err, listErr)
		}
		if findCalled {
			t.Error("find called after process listing failure")
		}
	})
}
