//go:build agent

package main

import (
	"testing"
)

// TestParseCommand verifies the full contract for ParseCommand:
//   - Known verbs are recognized and args are captured
//   - Unknown input returns ok=false (chat fallback)
//   - Leading/trailing whitespace is trimmed
//   - Empty input returns ok=false
func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantVerb string
		wantArgs []string
	}{
		{
			name:     "tag with platform",
			input:    "tag snes",
			wantOK:   true,
			wantVerb: "tag",
			wantArgs: []string{"snes"},
		},
		{
			// Parser succeeds; dispatcher is responsible for the usage error.
			name:     "tag without arg parses as known verb with no args",
			input:    "tag",
			wantOK:   true,
			wantVerb: "tag",
			wantArgs: nil,
		},
		{
			name:     "normalize",
			input:    "normalize",
			wantOK:   true,
			wantVerb: "normalize",
			wantArgs: nil,
		},
		{
			name:     "status",
			input:    "status",
			wantOK:   true,
			wantVerb: "status",
			wantArgs: nil,
		},
		{
			name:     "metrics",
			input:    "metrics",
			wantOK:   true,
			wantVerb: "metrics",
			wantArgs: nil,
		},
		{
			name:     "query with game name",
			input:    "query Axelay",
			wantOK:   true,
			wantVerb: "query",
			wantArgs: []string{"Axelay"},
		},
		{
			name:   "natural language falls through to chat",
			input:  "what games are on n64?",
			wantOK: false,
		},
		{
			name:   "empty input falls through to chat",
			input:  "",
			wantOK: false,
		},
		{
			name:     "extra whitespace is trimmed",
			input:    "  tag   n64  ",
			wantOK:   true,
			wantVerb: "tag",
			wantArgs: []string{"n64"},
		},
		{
			name:     "extra args are captured",
			input:    "tag snes extra",
			wantOK:   true,
			wantVerb: "tag",
			wantArgs: []string{"snes", "extra"},
		},
		{
			name:     "query with multi-word game captures all args",
			input:    "query Super Mario World",
			wantOK:   true,
			wantVerb: "query",
			wantArgs: []string{"Super", "Mario", "World"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := ParseCommand(tt.input)

			if ok != tt.wantOK {
				t.Fatalf("ParseCommand(%q): ok=%v want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return // chat fallback; Raw is set but Verb/Args are meaningless
			}

			if cmd.Verb != tt.wantVerb {
				t.Errorf("Verb=%q want %q", cmd.Verb, tt.wantVerb)
			}

			if len(cmd.Args) != len(tt.wantArgs) {
				t.Errorf("Args=%v (len %d) want %v (len %d)", cmd.Args, len(cmd.Args), tt.wantArgs, len(tt.wantArgs))
				return
			}
			for i, a := range cmd.Args {
				if a != tt.wantArgs[i] {
					t.Errorf("Args[%d]=%q want %q", i, a, tt.wantArgs[i])
				}
			}
		})
	}
}
