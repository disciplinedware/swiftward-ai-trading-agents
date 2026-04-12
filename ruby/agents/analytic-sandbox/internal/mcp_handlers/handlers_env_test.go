package mcp_handlers

import (
	"analytic-sandbox/internal/session"
	"reflect"
	"testing"
)

func TestBuildShellCommand(t *testing.T) {
	sess := &session.Session{}
	h := &Handlers{
		sess: sess,
	}

	tests := []struct {
		name     string
		params   ShellParams
		expected []string
	}{
		{
			name:     "Simple command",
			params:   ShellParams{Command: "echo hello"},
			expected: []string{"sh", "-c", "echo hello"},
		},
		{
			name:     "Command with quotes",
			params:   ShellParams{Command: `grep "pattern" file.txt`},
			expected: []string{"sh", "-c", `grep "pattern" file.txt`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.buildShellCommand(tt.params)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildShellCommand() = %v, want %v", got, tt.expected)
			}
		})
	}
}
