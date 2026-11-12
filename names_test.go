package main

import (
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParseProcessNames(t *testing.T) {
	long := strings.Repeat("x", 40)
	tests := []struct {
		name      string
		in        string
		self      string
		want      []string
		wantErr   error // checked with errors.Is
		wantAny   bool  // any error
		needLimit bool  // skip where names are not truncated
	}{
		{name: "single", in: "foo", self: "getgot", want: []string{"foo"}},
		{name: "several", in: "foo,bar", self: "getgot", want: []string{"foo", "bar"}},
		{name: "spaces are trimmed", in: " foo , bar ", self: "getgot", want: []string{"foo", "bar"}},
		{name: "duplicates removed", in: "foo,bar,foo", self: "getgot", want: []string{"foo", "bar"}},
		{name: "empty", in: "", self: "getgot", wantErr: errNoNames},
		{name: "blank", in: "  ", self: "getgot", wantErr: errNoNames},
		{name: "empty entry", in: "foo,,bar", self: "getgot", wantAny: true},
		{name: "trailing comma", in: "foo,", self: "getgot", wantAny: true},
		{name: "self", in: "foo,getgot", self: "getgot", wantAny: true},
		{name: "self with spaces", in: " getgot ", self: "getgot", wantAny: true},
		{name: "case sensitive", in: "GetGot", self: "getgot", want: []string{"GetGot"}},
		{name: "too long", in: "foo," + long, self: "getgot", wantAny: true, needLimit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needLimit && commLimit(runtime.GOOS) == 0 {
				t.Skipf("no process name limit on %s", runtime.GOOS)
			}
			got, err := parseProcessNames(tt.in, tt.self)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAny:
				if err == nil {
					t.Fatalf("got %q, want an error", got)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !slices.Equal(got, tt.want) {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestParseProcessNamesTruncatedSelf(t *testing.T) {
	limit := commLimit(runtime.GOOS)
	if limit == 0 {
		t.Skipf("no process name limit on %s", runtime.GOOS)
	}
	self := strings.Repeat("g", limit+5)
	if got, err := parseProcessNames(self[:limit], self); err == nil {
		t.Errorf("got %q, want an error for the truncated executable name", got)
	}
}

func TestCommLimit(t *testing.T) {
	tests := []struct {
		goos string
		want int
	}{
		{"linux", 15},
		{"darwin", 16},
		{"freebsd", 19},
		{"windows", 0},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := commLimit(tt.goos); got != tt.want {
				t.Errorf("commLimit(%q) = %d, want %d", tt.goos, got, tt.want)
			}
		})
	}
}
