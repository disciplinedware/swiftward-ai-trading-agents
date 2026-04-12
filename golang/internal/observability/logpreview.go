package observability

import "strings"

// LogPreview truncates s to maxLen runes for embedding in log message strings.
// Newlines are collapsed to spaces. Uses rune-aware truncation to avoid splitting
// multi-byte UTF-8 characters (which would cause OTLP protobuf serialization to
// reject the entire log batch).
func LogPreview(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "(empty)"
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
