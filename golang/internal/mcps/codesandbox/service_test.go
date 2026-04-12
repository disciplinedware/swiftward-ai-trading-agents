//go:build !integration

package codesandbox

import (
	"strings"
	"testing"
)

func TestValidateAgentID(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		wantErr bool
	}{
		{"valid id", "agent-001", false},
		{"valid alphanumeric", "agentABC123", false},
		{"empty", "", true},
		{"dot only", ".", true},
		{"slash", "agent/other", true},
		{"backslash", "agent\\other", true},
		{"double dot", "agent..other", true},
		{"path traversal", "../secret", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentID(tt.agentID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAgentID(%q) err=%v, wantErr=%v", tt.agentID, err, tt.wantErr)
			}
		})
	}
}

func TestParseDockerPort(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{"ipv4 format", "0.0.0.0:32768\n", 32768, false},
		{"ipv6 format", ":::32768\n", 32768, false},
		{"plain port", "32768\n", 32768, false},
		{"empty string", "", 0, true},
		{"non-numeric port", "0.0.0.0:abc\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDockerPort(tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDockerPort(%q) err=%v, wantErr=%v", tt.output, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parseDockerPort(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestParseDockerPortEdgeCases(t *testing.T) {
	// Verify the "empty after trim" path - a string that trims to empty.
	output := "   \n"
	line := strings.TrimSpace(output)
	parts := strings.Split(line, ":")
	// parts will be [""] - len 1, portStr will be "", Atoi will fail.
	if len(parts) == 0 {
		t.Fatal("unexpected: Split always returns at least one element")
	}
	_, err := parseDockerPort(output)
	if err == nil {
		t.Error("expected error for whitespace-only output, got nil")
	}
}
