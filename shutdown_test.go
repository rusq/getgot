//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rusq/gotsr"
)

const (
	shutdownHelperEnv     = "GETGOT_SHUTDOWN_HELPER"
	shutdownHelperPIDEnv  = "GETGOT_SHUTDOWN_PID_FILE"
	shutdownHelperLogEnv  = "GETGOT_SHUTDOWN_LOG_FILE"
	shutdownHelperTimeout = 8 * time.Second
)

func TestShutdownRemovesStateAfterLogging(t *testing.T) {
	if os.Getenv(shutdownHelperEnv) == "1" {
		runShutdownHelper(t)
		return
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "getgot.pid")
	logFile := filepath.Join(dir, "getgot.log")
	p, err := gotsr.New(gotsr.WithPIDFile(pidFile))
	if err != nil {
		t.Fatalf("gotsr.New(): %v", err)
	}
	helperStopped := false
	t.Cleanup(func() {
		if !helperStopped {
			cleanupShutdownHelper(t, p, pidFile, 2*time.Second)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), shutdownHelperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownRemovesStateAfterLogging$")
	cmd.Env = append(os.Environ(),
		shutdownHelperEnv+"=1",
		shutdownHelperPIDEnv+"="+pidFile,
		shutdownHelperLogEnv+"="+logFile,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("shutdown helper startup timed out: %v; output: %s", ctx.Err(), output.String())
		}
		t.Fatalf("start shutdown helper: %v; output: %s", err, output.String())
	}

	waitForShutdownCondition(t, 3*time.Second, "authenticated running state", func() (bool, error) {
		return p.IsRunning()
	})
	if err := p.Terminate(); err != nil {
		t.Fatalf("Terminate(): %v", err)
	}
	waitForShutdownCondition(t, 5*time.Second, "state-file cleanup", func() (bool, error) {
		_, err := os.Stat(pidFile)
		if err == nil {
			return false, nil
		}
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	})
	helperStopped = true

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read shutdown log: %v", err)
	}
	if count := strings.Count(string(data), `msg="`+shutdownLogEvent+`"`); count != 1 {
		t.Fatalf("shutdown log contains %q %d times, want exactly once: %q", shutdownLogEvent, count, data)
	}
	if running, err := p.IsRunning(); err != nil || running {
		t.Fatalf("IsRunning() after shutdown = %t, %v; want false, nil", running, err)
	}
}

func runShutdownHelper(t *testing.T) {
	t.Helper()
	pidFile := os.Getenv(shutdownHelperPIDEnv)
	logFile := os.Getenv(shutdownHelperLogEnv)
	if pidFile == "" || logFile == "" {
		t.Fatal("shutdown helper paths are empty")
	}

	f, logger, err := newFileLogger(logFile, true)
	if err != nil {
		t.Fatalf("newFileLogger(): %v", err)
	}
	defer f.Close()
	p, err := gotsr.New(gotsr.WithPIDFile(pidFile))
	if err != nil {
		t.Fatalf("gotsr.New(): %v", err)
	}
	p.AtExit(daemonAtExit(logger, f))

	child, err := p.TSR()
	if err != nil {
		t.Fatalf("TSR(): %v", err)
	}
	if !child {
		return
	}
	if err := logLifecycleEvent(logger, startupLogEvent, f); err != nil {
		t.Fatal(closeAfterStartupFailure(p, err))
	}
	select {}
}

func waitForShutdownCondition(t *testing.T, timeout time.Duration, description string, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := check()
		if err != nil {
			t.Fatalf("wait for %s: %v", description, err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func cleanupShutdownHelper(t *testing.T, p *gotsr.Process, pidFile string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := p.IsRunning()
		if err == nil && running {
			if err := p.Terminate(); err != nil {
				t.Errorf("cleanup Terminate(): %v", err)
				return
			}
			if !waitForShutdownBestEffort(pidFile, time.Until(deadline)) {
				t.Errorf("cleanup timed out waiting for state-file removal: %s", pidFile)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		return
	}
	if err := p.Close(); err != nil {
		t.Errorf("cleanup stale/unpublished state: %v", err)
	}
}

func waitForShutdownBestEffort(pidFile string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
