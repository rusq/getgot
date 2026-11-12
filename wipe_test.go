package main

import (
	"errors"
	"slices"
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
	if err := h.sys.killErr[h.pid]; err != nil {
		return err
	}
	h.sys.killed = append(h.sys.killed, h.pid)
	return nil
}

func (h fakeHandle) Release() error {
	h.sys.released = append(h.sys.released, h.pid)
	return nil
}

// fakeSystem lists procs, and looks them up in now, which reflects the state
// after the listing (a missing PID has exited, a different name is a reuse).
type fakeSystem struct {
	self     int
	procs    []fakeProc
	now      map[int]string
	listErr  error
	openErr  map[int]error
	killErr  map[int]error
	opened   []int
	killed   []int
	released []int
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
	name, ok := s.now[pid]
	if !ok {
		return nil, nil
	}
	return fakeProc{pid, name}, nil
}

func (s *fakeSystem) Open(pid int) (handle, error) {
	if err := s.openErr[pid]; err != nil {
		return nil, err
	}
	s.opened = append(s.opened, pid)
	return fakeHandle{s, pid}, nil
}

func (s *fakeSystem) Getpid() int { return s.self }

func TestWipe(t *testing.T) {
	errDenied := errors.New("operation not permitted")
	errList := errors.New("list failed")
	tests := []struct {
		name       string
		sys        fakeSystem
		names      []string
		wantKilled []int
		wantErrs   []error // each must be in the joined error
		wantNoErr  bool
	}{
		{
			name: "kills every match",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}, {11, "bar"}, {12, "foo"}, {13, "baz"}},
				now:   map[int]string{10: "foo", 11: "bar", 12: "foo", 13: "baz"},
			},
			names:      []string{"foo", "baz"},
			wantKilled: []int{10, 12, 13},
			wantNoErr:  true,
		},
		{
			name: "failed kill does not stop the scan",
			sys: fakeSystem{
				procs:   []fakeProc{{10, "foo"}, {11, "foo"}, {12, "foo"}},
				now:     map[int]string{10: "foo", 11: "foo", 12: "foo"},
				killErr: map[int]error{10: errDenied},
			},
			names:      []string{"foo"},
			wantKilled: []int{11, 12},
			wantErrs:   []error{errDenied},
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
			wantKilled: []int{11},
			wantErrs:   []error{errDenied, errList},
		},
		{
			name: "reused PID is not killed",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}, {11, "foo"}},
				now:   map[int]string{10: "innocent", 11: "foo"},
			},
			names:      []string{"foo"},
			wantKilled: []int{11},
			wantNoErr:  true,
		},
		{
			name: "exited process is skipped",
			sys: fakeSystem{
				procs: []fakeProc{{10, "foo"}},
				now:   map[int]string{},
			},
			names:     []string{"foo"},
			wantNoErr: true,
		},
		{
			name: "self is never killed",
			sys: fakeSystem{
				self:  10,
				procs: []fakeProc{{10, "foo"}, {11, "foo"}},
				now:   map[int]string{10: "foo", 11: "foo"},
			},
			names:      []string{"foo"},
			wantKilled: []int{11},
			wantNoErr:  true,
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
			pm := make(map[string]bool)
			for _, n := range tt.names {
				pm[n] = true
			}
			err := wipe(&tt.sys, pm)
			if tt.wantNoErr && err != nil {
				t.Errorf("wipe() error = %v, want nil", err)
			}
			for _, want := range tt.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("wipe() error = %v, want it to include %v", err, want)
				}
			}
			if !slices.Equal(tt.sys.killed, tt.wantKilled) {
				t.Errorf("killed %v, want %v", tt.sys.killed, tt.wantKilled)
			}
			if !slices.Equal(tt.sys.released, tt.sys.opened) {
				t.Errorf("released %v, want every opened handle %v", tt.sys.released, tt.sys.opened)
			}
		})
	}
}
