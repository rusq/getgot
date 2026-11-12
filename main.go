package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/rusq/gotsr"
)

const (
	defKillInterval = 5 * time.Second
	// shutdownTimeout is how long the daemon waits for gotsr to exit the
	// process after the context is cancelled.
	shutdownTimeout = 10 * time.Second
)

var (
	names    = flag.String("names", "jamf,Nudge,du", "comma separated list of process executable names to kill, case sensitive; names are matched as the kernel reports them (15 characters on Linux, 16 on macOS)")
	interval = flag.Duration("every", defKillInterval, "interval to check for processes")
	logName  = flag.String("log", "", "log file `path` (default <user cache dir>/getgot/getgot.log)")
	stop     = flag.Bool("stop", false, "stop the getgot daemon")
)

func main() {
	flag.Parse()
	if *interval <= 0*time.Second {
		*interval = defKillInterval
	}

	pidFile, err := pidPath()
	if err != nil {
		log.Fatal(err)
	}
	p, err := gotsr.New(gotsr.WithPIDFile(pidFile))
	if err != nil {
		log.Fatal(err)
	}
	if *stop {
		if err := p.Terminate(); err != nil {
			if errors.Is(err, gotsr.ErrNotRunning) {
				slog.Warn("not running")
				return
			}
			log.Fatal(err)
		}
		slog.Info("terminated")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.AtExit(func() {
		slog.Info("stopping")
		cancel() // cancel the context before the process exits
	})

	// os.Executable resolves symlinks on Linux, while the kernel reports the
	// name the program was started as, so reject both.
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	processes, err := parseProcessNames(*names, filepath.Base(exe), filepath.Base(os.Args[0]))
	if err != nil {
		log.Fatalf("invalid -names: %s", err)
	}

	// Open the log in every stage, before TSR, so that a bad path fails in
	// the foreground, before the daemon is reported as started.
	logFile, err := logPath(*logName)
	if err != nil {
		log.Fatal(err)
	}
	f, err := openLog(logFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// TSR checks the lock in every stage.  A launch that loses a race with
	// another launch also gets ErrAlreadyRunning.
	child, err := p.TSR()
	if errors.Is(err, gotsr.ErrAlreadyRunning) {
		slog.Warn("getgot already running")
		return
	} else if err != nil {
		log.Fatal(err)
	}
	if !child {
		slog.Info("starting getgot...", "log", logFile)
		return
	}

	setLogOutput(f)
	slog.Info("started", "pid", os.Getpid(), "names", processes, "every", *interval)
	supress(ctx, *interval, stateFiles(pidFile), processes...)
	// The context is cancelled by the AtExit hook.  gotsr then releases the
	// lock, removes the PID file and exits the process, so wait for it
	// instead of returning, which would exit before it is done.
	time.Sleep(shutdownTimeout)
	slog.Error("gotsr did not exit the process", "timeout", shutdownTimeout)
	os.Exit(1)
}

// supress kills the processes named procNames every interval, and keeps the
// files in keep fresh, until ctx is cancelled, which is a normal stop.
func supress(ctx context.Context, interval time.Duration, keep []string, procNames ...string) {
	pm := make(map[string]bool, len(procNames))
	for _, v := range procNames {
		pm[v] = true
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			touch(keep...)
			if err := wipe(osSystem{}, pm); err != nil {
				slog.Error("wipe", "error", err)
			}
		}
	}
}
