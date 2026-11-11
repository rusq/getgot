package main

import (
	"reflect"
	"strings"
	"testing"
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
