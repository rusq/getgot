package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

var errNoNames = errors.New("no process names given")

// commLimit returns the length to which the kernel truncates process names
// reported by go-ps on the platform goos, or 0 if there is no known limit.
func commLimit(goos string) int {
	switch goos {
	case "linux":
		return 15 // TASK_COMM_LEN - 1
	case "darwin":
		return 16 // MAXCOMLEN
	case "freebsd":
		return 19 // COMMLEN
	default:
		return 0
	}
}

// parseProcessNames splits the comma separated list s, trims spaces, and
// removes duplicates, preserving order.  It rejects empty entries, names
// longer than the platform limit, which would never match, and the names self
// of the getgot executable, so that getgot never kills its own instances.
func parseProcessNames(s string, self ...string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, errNoNames
	}
	limit := commLimit(runtime.GOOS)
	isSelf := make(map[string]bool, len(self))
	for _, name := range self {
		if limit > 0 && len(name) > limit {
			name = name[:limit]
		}
		isSelf[name] = true
	}
	var (
		names []string
		seen  = make(map[string]bool)
	)
	for i, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		switch {
		case name == "":
			return nil, fmt.Errorf("entry %d is empty", i+1)
		case limit > 0 && len(name) > limit:
			return nil, fmt.Errorf("%q is longer than %d characters, and would never match on %s", name, limit, runtime.GOOS)
		case isSelf[name]:
			return nil, fmt.Errorf("%q is the getgot executable", name)
		case seen[name]:
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}
