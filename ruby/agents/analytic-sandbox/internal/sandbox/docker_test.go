package sandbox

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildTimeoutCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      []string
		timeout  time.Duration
		expected []string
	}{
		{
			name:     "Simple command",
			cmd:      []string{"echo", "hello"},
			timeout:  10 * time.Second,
			expected: []string{"timeout", "-s", "KILL", "10s", "echo", "hello"},
		},
		{
			name:     "Command with flags",
			cmd:      []string{"ls", "-la", "/tmp"},
			timeout:  5 * time.Second,
			expected: []string{"timeout", "-s", "KILL", "5s", "ls", "-la", "/tmp"},
		},
		{
			name:     "Minimum timeout",
			cmd:      []string{"sleep", "1"},
			timeout:  500 * time.Millisecond,
			expected: []string{"timeout", "-s", "KILL", "1s", "sleep", "1"},
		},
		{
			name:     "Chained command in sh -c",
			cmd:      []string{"sh", "-c", "head -n 1000 | go run solution.go"},
			timeout:  120 * time.Second,
			expected: []string{"timeout", "-s", "KILL", "120s", "sh", "-c", "head -n 1000 | go run solution.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTimeoutCommand(tt.cmd, tt.timeout)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildTimeoutCommand() = %v, want %v", got, tt.expected)
			}
		})
	}
}
