package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/rusq/gotsr"
)

const (
	defKillInterval  = 5 * time.Second
	defLogName       = "getgot.log"
	stateDirName     = "getgot"
	pidFileName      = "getgot.pid"
	startupLogEvent  = "getgot started"
	shutdownLogEvent = "getgot stopping"
)

var (
	names    = flag.String("names", "jamf,Nudge,du", "comma separated list of process executable names to kill, case sensitive")
	interval = flag.Duration("every", defKillInterval, "interval to check for processes")
	logName  = flag.String("log", defLogName, "log file name")
	stop     = flag.Bool("stop", false, "stop the getgot daemon")
)

func main() {
	flag.Parse()
	if *interval <= 0*time.Second {
		*interval = defKillInterval
	}
	if *logName == "" {
		*logName = defLogName
	}

	pidFile, err := statePIDFile(os.UserConfigDir)
	if err != nil {
		log.Fatal(err)
	}
	selectedLogFile, privateLog := selectLogFile(*logName, filepath.Dir(pidFile))
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
	if running, err := p.IsRunning(); err != nil {
		log.Fatal(err)
	} else if running {
		slog.Warn("getgot already running")
		return
	}

	processes, err := parseProcessNames(*names)
	if err != nil {
		log.Fatal(err)
	}

	f, daemonLogger, err := newFileLogger(selectedLogFile, privateLog)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	p.AtExit(daemonAtExit(daemonLogger, f))

	child, err := p.TSR()
	if err != nil {
		log.Fatal(err)
	}
	if !child {
		slog.Info("starting getgot...")
		return
	}

	installDefaultLogging(daemonLogger, f)
	if err := logLifecycleEvent(daemonLogger, startupLogEvent, f); err != nil {
		log.Fatal(closeAfterStartupFailure(p, err))
	}
	if err := supress(ctx, *interval, processes...); err != nil {
		log.Fatal(err)
	}
}

func statePIDFile(userConfigDir func() (string, error)) (string, error) {
	baseDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	if !filepath.IsAbs(baseDir) {
		return "", fmt.Errorf("user config directory %q is not absolute", baseDir)
	}

	stateDir := filepath.Join(baseDir, stateDirName)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create state directory %q: %w", stateDir, err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("secure state directory %q: %w", stateDir, err)
	}
	return filepath.Join(stateDir, pidFileName), nil
}

func selectLogFile(configured, stateDir string) (filename string, private bool) {
	if configured == "" || configured == defLogName {
		return filepath.Join(stateDir, defLogName), true
	}
	return configured, false
}

func newFileLogger(filename string, private bool) (*os.File, *slog.Logger, error) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file %q: %w", filename, err)
	}
	if private {
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return nil, nil, fmt.Errorf("secure log file %q: %w", filename, err)
		}
	}
	return f, slog.New(slog.NewTextHandler(f, nil)), nil
}

func installDefaultLogging(logger *slog.Logger, f *os.File) {
	slog.SetDefault(logger)
	log.SetOutput(f)
}

func logLifecycleEvent(logger *slog.Logger, message string, f *os.File) error {
	logger.Info(message)
	if f == nil {
		return nil
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync log after %q: %w", message, err)
	}
	return nil
}

func daemonAtExit(logger *slog.Logger, logFile *os.File) func() {
	return func() {
		if err := logLifecycleEvent(logger, shutdownLogEvent, logFile); err != nil {
			logger.Error("shutdown log", "error", err)
		}
	}
}

type runtimeCloser interface {
	Close() error
}

func closeAfterStartupFailure(closer runtimeCloser, startupErr error) error {
	if err := closer.Close(); err != nil {
		return errors.Join(startupErr, fmt.Errorf("close daemon runtime after startup log failure: %w", err))
	}
	return startupErr
}

func parseProcessNames(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	processes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for i, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("process name %d is empty", i+1)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		processes = append(processes, name)
	}
	return processes, nil
}

func supress(ctx context.Context, interval time.Duration, procNames ...string) error {
	pm := make(map[string]bool, len(procNames))
	for _, v := range procNames {
		pm[v] = true
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := wipe(pm); err != nil {
				slog.Error("wipe", "error", err)
			}
		}
	}
}

// wipe kills all processes in the pm map
func wipe(pm map[string]bool) error {
	return wipeProcesses(pm, ps.Processes, findProcess)
}

type processKiller interface {
	Kill() error
}

func findProcess(pid int) (processKiller, error) {
	return os.FindProcess(pid)
}

func wipeProcesses(pm map[string]bool, list func() ([]ps.Process, error), find func(int) (processKiller, error)) error {
	pp, err := list()
	if err != nil {
		return fmt.Errorf("error listing processes: %w", err)
	}

	var errs []error
	for _, p := range pp {
		if _, ok := pm[p.Executable()]; !ok {
			continue
		}
		proc, err := find(p.Pid())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to find process %d: %w", p.Pid(), err))
			continue
		}
		if err := proc.Kill(); err != nil {
			errs = append(errs, fmt.Errorf("failed to kill %s (PID %d): %w", p.Executable(), p.Pid(), err))
			continue
		}
		slog.Info("killed", "executable", p.Executable(), "pid", p.Pid(), "ppid", p.PPid())
	}
	return errors.Join(errs...)
}
