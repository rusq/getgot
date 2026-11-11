package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mitchellh/go-ps"
)

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
