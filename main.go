package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/rusq/gotsr"
)

const (
	defKillInterval = 5 * time.Second
	defLogName      = "getgot.log"
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

	p, err := gotsr.New()
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.AtExit(func() { cancel() }) // cancel the context before the process exits

	processes := strings.Split(*names, ",")
	if len(processes) == 0 {
		log.Fatal("no processes to kill, -names is empty")
	}

	child, err := p.TSR()
	if err != nil {
		log.Fatal(err)
	}
	if !child {
		slog.Info("starting getgot...")
		return
	}

	f, err := os.OpenFile(*logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	log.SetOutput(f)
	if err := supress(ctx, *interval, processes...); err != nil {
		log.Fatal(err)
	}
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
	pp, err := ps.Processes()
	if err != nil {
		return fmt.Errorf("error listing processes: %w", err)
	}

	for _, p := range pp {
		if _, ok := pm[p.Executable()]; !ok {
			continue
		}
		proc, err := os.FindProcess(p.Pid())
		if err != nil {
			return fmt.Errorf("unable to find process %d: %w", p.Pid(), err)
		}
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("failed to kill %s: %w", p.Executable(), err)
		}
		slog.Info("killed", "executable", p.Executable(), "pid", p.Pid(), "ppid", p.PPid())
	}
	return nil
}
