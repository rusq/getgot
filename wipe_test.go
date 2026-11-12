package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mitchellh/go-ps"
)

type fakeProc struct {
	pid  int
	name string
}

func (p fakeProc) Pid() int           { return p.pid }
func (p fakeProc) PPid() int          { return 1 }
func (p fakeProc) Executable() string { return p.name }

type fakeHandle struct {
	sys *fakeSystem
	pid int
}

func (h fakeHandle) Kill() error {
	h.sys.event("kill", h.pid)
	return h.sys.killErr[h.pid]
}

func (h fakeHandle) Release() error {
	h.sys.event("release", h.pid)
	return nil
}

// fakeSystem lists procs, and looks them up in now, which reflects the state
// after the listing (a missing PID has exited, a different name is a reuse).
// It records every call in events.
type fakeSystem struct {
	self      int
	procs     []fakeProc
	now       map[int]string
	listErr   error
	openErr   map[int]error
	lookupErr map[int]error
	killErr   map[int]error
	events    []string
}

func (s *fakeSystem) event(what string, pid int) {
	s.events = append(s.events, fmt.Sprintf("%s %d", what, pid))
}

func (s *fakeSystem) Processes() ([]ps.Process, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []ps.Process
	for _, p := range s.procs {
		out = append(out, p)
	}
	return out, nil
}

func (s *fakeSystem) Lookup(pid int) (ps.Process, error) {
	s.event("lookup", pid)
	if err := s.lookupErr[pid]; err != nil {
		return nil, err
	}
	name, ok := s.now[pid]
	if !ok {
		return nil, nil
	}
	return fakeProc{pid, name}, nil
}

func (s *fakeSystem) Open(pid int) (handle, error) {
	s.event("open", pid)
	if err := s.openErr[pid]; err != nil {
		return nil, err
	}
	return fakeHandle{s, pid}, nil
}

func (s *fakeSystem) Getpid() int { return s.self }

// ev builds the expected event log.
func ev(events ...string) []string { return events }

// killed is the event sequence of a successful kill of pid.
func killed(pid int) []string {
	return ev(fmt.Sprint("open ", pid), fmt.Sprint("lookup ", pid), fmt.Sprint("kill ", pid), fmt.Sprint("release ", pid))
}

// skipped is the event sequence of a process found gone or reused.
func skipped(pid int) []string {
	return ev(fmt.Sprint("open ", pid), fmt.Sprint("lookup ", pid), fmt.Sprint("release ", pid))
}

// captureLog sends slog and log output to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	old, oldW, oldF := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(old)
		log.SetOutput(oldW)
		log.SetFlags(oldF)
	})
	var buf bytes.Buffer
	setLogOutput(&buf)
	return &buf
}

func TestWipe(t *testing.T) {
	errDenied := errors.New("operation not permitted")
	errList := errors.New("list failed")
	tests := []struct {
		name       string
		sys        fakeSystem
		names      []string
		wantEvents []string
		wantKilled []int   // PIDs logged as killed
		wantErrs   []error // each must be in the joined error
		wantInErr  []string
	}{
		{
			name: "kills every match",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}, {11, "bar"}, {12, "foo"}, {13, "baz"}},
				now:   map[int]string{10: "foo", 11: "bar", 12: "foo", 13: "baz"},
			},
			names:      []string{"foo", "baz"},
			wantEvents: slices.Concat(killed(10), killed(12), killed(13)),
			wantKilled: []int{10, 12, 13},
		},
		{
			name: "failed kill does not stop the scan",
			sys: fakeSystem{
				procs:   []fakeProc{{10, "foo"}, {11, "foo"}, {12, "foo"}},
				now:     map[int]string{10: "foo", 11: "foo", 12: "foo"},
				killErr: map[int]error{10: errDenied},
			},
			names:      []string{"foo"},
			wantEvents: slices.Concat(killed(10), killed(11), killed(12)),
			wantKilled: []int{11, 12},
			wantErrs:   []error{errDenied},
			wantInErr:  []string{"kill foo (pid 10)"},
		},
		{
			name: "all failures are joined",
			sys: fakeSystem{
				procs:   []fakeProc{{10, "foo"}, {11, "foo"}, {12, "foo"}},
				now:     map[int]string{10: "foo", 11: "foo", 12: "foo"},
				openErr: map[int]error{10: errDenied},
				killErr: map[int]error{12: errList},
			},
			names:      []string{"foo"},
			wantEvents: slices.Concat(ev("open 10"), killed(11), killed(12)),
			wantKilled: []int{11},
			wantErrs:   []error{errDenied, errList},
			wantInErr:  []string{"pid 10", "pid 12"},
		},
		{
			name: "reused PID is not killed",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}, {11, "foo"}},
				now:   map[int]string{10: "innocent", 11: "foo"},
			},
			names:      []string{"foo"},
			wantEvents: slices.Concat(skipped(10), killed(11)),
			wantKilled: []int{11},
		},
		{
			name: "exited process is skipped",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}},
				now:   map[int]string{},
			},
			names:      []string{"foo"},
			wantEvents: skipped(10),
		},
		{
			name: "process exiting during lookup is skipped",
			sys: fakeSystem{
				procs:     []fakeProc{{10, "foo"}},
				now:       map[int]string{10: "foo"},
				lookupErr: map[int]error{10: fmt.Errorf("open /proc/10/stat: %w", fs.ErrNotExist)},
			},
			names:      []string{"foo"},
			wantEvents: skipped(10),
		},
		{
			name: "lookup error is reported",
			sys: fakeSystem{
				procs:     []fakeProc{{10, "foo"}, {11, "foo"}},
				now:       map[int]string{10: "foo", 11: "foo"},
				lookupErr: map[int]error{10: errDenied},
			},
			names:      []string{"foo"},
			wantEvents: slices.Concat(skipped(10), killed(11)),
			wantKilled: []int{11},
			wantErrs:   []error{errDenied},
		},
		{
			name: "process done at kill is not an error",
			sys: fakeSystem{
				procs:   []fakeProc{{10, "foo"}},
				now:     map[int]string{10: "foo"},
				killErr: map[int]error{10: fmt.Errorf("signal: %w", os.ErrProcessDone)},
			},
			names:      []string{"foo"},
			wantEvents: killed(10),
		},
		{
			name: "self is never opened",
			sys: fakeSystem{
				self:  10,
				procs: []fakeProc{{10, "foo"}, {11, "foo"}},
				now:   map[int]string{10: "foo", 11: "foo"},
			},
			names:      []string{"foo"},
			wantEvents: killed(11),
			wantKilled: []int{11},
		},
		{
			name:     "listing error",
			sys:      fakeSystem{listErr: errList},
			names:    []string{"foo"},
			wantErrs: []error{errList},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLog(t)
			pm := make(map[string]bool)
			for _, n := range tt.names {
				pm[n] = true
			}
			err := wipe(&tt.sys, pm)
			if len(tt.wantErrs) == 0 && err != nil {
				t.Errorf("wipe() error = %v, want nil", err)
			}
			for _, want := range tt.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("wipe() error = %v, want it to include %v", err, want)
				}
			}
			for _, want := range tt.wantInErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("wipe() error = %v, want it to mention %q", err, want)
				}
			}
			if !slices.Equal(tt.sys.events, tt.wantEvents) {
				t.Errorf("events:\n got %q\nwant %q", tt.sys.events, tt.wantEvents)
			}
			if got := strings.Count(logs.String(), "msg=killed"); got != len(tt.wantKilled) {
				t.Errorf("%d kills logged, want %d:\n%s", got, len(tt.wantKilled), logs)
			}
			for _, pid := range tt.wantKilled {
				if !strings.Contains(logs.String(), fmt.Sprintf("pid=%d ", pid)) {
					t.Errorf("kill of %d not logged:\n%s", pid, logs)
				}
			}
		})
	}
}
