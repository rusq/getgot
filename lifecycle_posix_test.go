//go:build !windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rusq/gotsr"
)

// helperEnv makes the test binary act as a disposable target process that
// sleeps until it is killed, see TestMain.
const helperEnv = "GETGOT_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// env is an isolated environment for the real getgot binary: its own state
// directory, log file and process names.
type env struct {
	t       *testing.T
	bin     string // getgot binary
	tmp     string // TMPDIR, holds the state directory
	logFile string
	pidFile string
	target  string // unique name of the helper processes getgot kills
}

// buildGetgot builds the getgot binary into dir.
func buildGetgot(t *testing.T, dir string) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not found")
	}
	bin := filepath.Join(dir, "getgot")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, gobin, "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func uniqueName(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return prefix + hex.EncodeToString(b) // at most 15 characters for short prefixes
}

func newEnv(t *testing.T, bin string) *env {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		t:       t,
		bin:     bin,
		tmp:     tmp,
		logFile: filepath.Join(tmp, "getgot.log"),
		target:  uniqueName(t, "ggt"),
	}
	e.pidFile = e.statePID()
	t.Cleanup(e.cleanup)
	return e
}

// statePID computes the PID file path the way getgot does in this
// environment.
func (e *env) statePID() string {
	e.t.Helper()
	e.t.Setenv("TMPDIR", e.tmp)
	e.t.Setenv("XDG_RUNTIME_DIR", "")
	p, err := pidPath()
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) environ() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, "XDG_RUNTIME_DIR=") || strings.HasPrefix(kv, helperEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TMPDIR="+e.tmp)
}

// run runs getgot with args in a new working directory, and returns its
// combined output and error.
func (e *env) run(args ...string) (string, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Dir = e.t.TempDir()
	cmd.Env = e.environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// start starts the daemon, killing the target processes.
func (e *env) start() (string, error) {
	return e.run("-names="+e.target, "-every=50ms", "-log="+e.logFile)
}

func (e *env) process() *gotsr.Process {
	e.t.Helper()
	p, err := gotsr.New(gotsr.WithPIDFile(e.pidFile))
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

// daemonPID returns the PID of the running daemon, or 0.
func (e *env) daemonPID() int {
	e.t.Helper()
	pid, err := e.process().PID()
	if errors.Is(err, gotsr.ErrNotRunning) {
		return 0
	} else if err != nil {
		e.t.Fatal(err)
	}
	return pid
}

// cleanup stops the daemon of this environment if it is still running.
func (e *env) cleanup() {
	if pid, err := e.process().PID(); err == nil && pid > 0 {
		if err := e.process().Terminate(); err != nil {
			e.t.Errorf("cleanup: terminate daemon %d: %v", pid, err)
		}
	}
}

// proc is a started helper process.  A single goroutine waits for it.
type proc struct {
	cmd  *exec.Cmd
	done chan struct{} // closed when the process has exited
}

// helper starts a disposable helper process named name.
func (e *env) helper(name string) *proc {
	e.t.Helper()
	self, err := os.Executable()
	if err != nil {
		e.t.Fatal(err)
	}
	bin := filepath.Join(e.tmp, name)
	if _, err := os.Stat(bin); err != nil {
		copyFile(e.t, self, bin)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(e.environ(), helperEnv+"=1")
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	p := &proc{cmd: cmd, done: make(chan struct{})}
	go func() { cmd.Wait(); close(p.done) }()
	e.t.Cleanup(func() {
		cmd.Process.Kill()
		<-p.done
	})
	return p
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// waitExit waits for p to exit and returns its wait status.
func waitExit(t *testing.T, p *proc, timeout time.Duration) (syscall.WaitStatus, bool) {
	t.Helper()
	select {
	case <-p.done:
		ws, _ := p.cmd.ProcessState.Sys().(syscall.WaitStatus)
		return ws, true
	case <-time.After(timeout):
		return 0, false
	}
}

func (e *env) log() string {
	e.t.Helper()
	b, err := os.ReadFile(e.logFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		e.t.Fatal(err)
	}
	return string(b)
}

func mustSucceed(t *testing.T, what, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v\n%s", what, err, out)
	}
}

func TestLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the getgot binary")
	}
	bin := buildGetgot(t, t.TempDir())

	t.Run("repeated start leaves the daemon running", func(t *testing.T) {
		e := newEnv(t, bin)
		out, err := e.start()
		mustSucceed(t, "start", out, err)
		first := e.daemonPID()
		if first == 0 {
			t.Fatalf("daemon not running after start\n%s", out)
		}
		out, err = e.start() // from another working directory
		mustSucceed(t, "second start", out, err)
		if !strings.Contains(out, "already running") {
			t.Errorf("second start output = %q, want \"already running\"", out)
		}
		if got := e.daemonPID(); got != first {
			t.Errorf("daemon PID = %d after second start, want %d", got, first)
		}
	})

	t.Run("simultaneous starts run one daemon", func(t *testing.T) {
		e := newEnv(t, bin)
		const n = 4
		errs := make(chan error, n)
		outs := make(chan string, n)
		for range n {
			go func() {
				out, err := e.start()
				outs <- out
				errs <- err
			}()
		}
		var started, already int
		for range n {
			out, err := <-outs, <-errs
			if err != nil {
				t.Errorf("start: %v\n%s", err, out)
			}
			switch {
			case strings.Contains(out, "already running"):
				already++
			case strings.Contains(out, "starting getgot"):
				started++
			}
		}
		if started != 1 || already != n-1 {
			t.Errorf("%d launches started the daemon and %d found it running, want 1 and %d", started, already, n-1)
		}
		if e.daemonPID() == 0 {
			t.Fatal("no daemon running")
		}
	})

	t.Run("kills the target and logs it", func(t *testing.T) {
		e := newEnv(t, bin)
		target := e.helper(e.target)
		bystander := e.helper(uniqueName(t, "ggb"))
		out, err := e.start()
		mustSucceed(t, "start", out, err)
		ws, exited := waitExit(t, target, 10*time.Second)
		if !exited {
			t.Fatal("target was not killed")
		}
		if ws.Signal() != syscall.SIGKILL {
			t.Errorf("target exit status = %v, want SIGKILL", ws)
		}
		if err := bystander.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Errorf("bystander: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(e.log(), "killed") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if log := e.log(); !strings.Contains(log, "killed") || !strings.Contains(log, e.target) {
			t.Errorf("log does not record the kill:\n%s", log)
		}
	})

	t.Run("stop from another directory", func(t *testing.T) {
		e := newEnv(t, bin)
		out, err := e.start()
		mustSucceed(t, "start", out, err)
		out, err = e.run("-stop")
		mustSucceed(t, "stop", out, err)
		if !strings.Contains(out, "terminated") {
			t.Errorf("stop output = %q, want \"terminated\"", out)
		}
		if pid := e.daemonPID(); pid != 0 {
			t.Errorf("daemon %d still running after -stop", pid)
		}
		out, err = e.run("-stop")
		mustSucceed(t, "second stop", out, err)
		if !strings.Contains(out, "not running") {
			t.Errorf("second stop output = %q, want \"not running\"", out)
		}
	})

	t.Run("clean shutdown", func(t *testing.T) {
		e := newEnv(t, bin)
		out, err := e.start()
		mustSucceed(t, "start", out, err)
		out, err = e.run("-stop")
		mustSucceed(t, "stop", out, err)
		log := e.log()
		for _, want := range []string{"msg=started", "msg=stopping"} {
			if !strings.Contains(log, want) {
				t.Errorf("log does not contain %q:\n%s", want, log)
			}
		}
		for _, bad := range []string{"context canceled", "level=ERROR"} {
			if strings.Contains(log, bad) {
				t.Errorf("log contains %q:\n%s", bad, log)
			}
		}
		if _, err := os.Stat(e.pidFile); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("PID file left after stop: %v", err)
		}
	})

	t.Run("stale PID file signals nobody", func(t *testing.T) {
		e := newEnv(t, bin)
		victim := e.helper(uniqueName(t, "ggv"))
		if err := os.WriteFile(e.pidFile, []byte(strconv.Itoa(victim.cmd.Process.Pid)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := e.run("-stop")
		mustSucceed(t, "stop", out, err)
		if !strings.Contains(out, "not running") {
			t.Errorf("stop output = %q, want \"not running\"", out)
		}
		out, err = e.start()
		mustSucceed(t, "start", out, err)
		if e.daemonPID() == 0 {
			t.Fatal("daemon did not start over a stale PID file")
		}
		if _, exited := waitExit(t, victim, 300*time.Millisecond); exited {
			t.Error("the process named in the stale PID file exited")
		}
	})

	t.Run("bad log path fails in the foreground", func(t *testing.T) {
		e := newEnv(t, bin)
		out, err := e.run("-names="+e.target, "-log="+filepath.Join(e.tmp, "no", "such", "x.log"))
		if err == nil {
			t.Fatalf("start succeeded with a bad log path\n%s", out)
		}
		if e.daemonPID() != 0 {
			t.Error("daemon running despite the bad log path")
		}
	})
}
