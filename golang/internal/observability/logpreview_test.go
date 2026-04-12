package observability

import "testing"

func TestLogPreview(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"under limit", "hello world", 20, "hello world"},
		{"over limit", "hello world this is long", 11, "hello world..."},
		{"exact limit", "hello", 5, "hello"},
		{"empty", "", 10, "(empty)"},
		{"whitespace only", "   ", 10, "(empty)"},
		{"newlines collapsed", "line1\nline2\nline3", 50, "line1 line2 line3"},
		{"trimmed", "  padded  ", 20, "padded"},
		{"multibyte rune aware", "привет мир!", 6, "привет..."},
		{"emoji truncation", "🎉🎊🎈🎁🎂", 3, "🎉🎊🎈..."},
		{"zero maxLen", "hello", 0, ""},
		{"negative maxLen", "hello", -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogPreview(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("LogPreview(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
