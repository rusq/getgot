package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/mitchellh/go-ps"
)

// handle is a process handle that can be killed.
type handle interface {
	Kill() error
	Release() error
}

// system lists, finds and opens processes.  It lets tests cover wipe without
// killing real processes.
type system interface {
	// Processes lists the running processes.
	Processes() ([]ps.Process, error)
	// Lookup returns the process with the PID, or nil if there is none.
	Lookup(pid int) (ps.Process, error)
	// Open returns a handle to the process with the PID.
	Open(pid int) (handle, error)
	// Getpid returns the PID of the calling process.
	Getpid() int
}

// osSystem is the real system.
type osSystem struct{}

func (osSystem) Processes() ([]ps.Process, error)   { return ps.Processes() }
func (osSystem) Lookup(pid int) (ps.Process, error) { return ps.FindProcess(pid) }
func (osSystem) Getpid() int                        { return os.Getpid() }
func (osSystem) Open(pid int) (handle, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// wipe kills all processes whose executable name is in pm, except the calling
// process.  It tries every match, and returns the joined errors of the
// failed kills.
func wipe(sys system, pm map[string]bool) error {
	pp, err := sys.Processes()
	if err != nil {
		return fmt.Errorf("error listing processes: %w", err)
	}
	self := sys.Getpid()

	var errs []error
	for _, p := range pp {
		name := p.Executable()
		if !pm[name] || p.Pid() == self {
			continue
		}
		killed, err := kill(sys, p.Pid(), name)
		if err != nil {
			errs = append(errs, fmt.Errorf("kill %s (pid %d): %w", name, p.Pid(), err))
			continue
		}
		if killed {
			slog.Info("killed", "executable", name, "pid", p.Pid(), "ppid", p.PPid())
		}
	}
	return errors.Join(errs...)
}

// kill kills the process pid if it is still named name.  It reports false if
// the process has gone.
//
// The name is checked again after the handle is obtained.  On Linux the
// handle is a pidfd, so a process that reused the PID before that check is
// never killed.  Elsewhere a small window remains between the check and the
// kill.
func kill(sys system, pid int, name string) (bool, error) {
	h, err := sys.Open(pid)
	if err != nil {
		return false, err
	}
	defer h.Release()

	again, err := sys.Lookup(pid)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil // exited while it was being looked up
	} else if err != nil {
		return false, err
	}
	if again == nil || again.Executable() != name {
		return false, nil // exited, and maybe reused
	}
	if err := h.Kill(); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
