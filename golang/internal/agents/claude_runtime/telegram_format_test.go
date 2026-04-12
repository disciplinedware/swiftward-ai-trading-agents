package claude_runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain text", in: "hello world", want: "hello world"},
		{name: "bold stars", in: "**bold**", want: "<b>bold</b>"},
		{name: "bold underscores", in: "__bold__", want: "<b>bold</b>"},
		{name: "italic", in: "_italic_", want: "<i>italic</i>"},
		{name: "strikethrough", in: "~~strike~~", want: "<s>strike</s>"},
		{name: "inline code", in: "use `fmt.Println`", want: "use <code>fmt.Println</code>"},
		{name: "code block", in: "```go\nfmt.Println()\n```", want: "<pre><code>fmt.Println()\n</code></pre>"},
		{name: "heading stripped", in: "## Title", want: "Title"},
		{name: "link", in: "[click](http://example.com)", want: `<a href="http://example.com">click</a>`},
		{name: "list items", in: "- one\n- two", want: "\u2022 one\n\u2022 two"},
		{name: "html escaped", in: "<script>alert(1)</script>", want: "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{name: "code preserves html", in: "`<div>`", want: "<code>&lt;div&gt;</code>"},
		{name: "blockquote stripped", in: "> quoted text", want: "quoted text"},
		{name: "javascript link stripped", in: "[click](javascript:void)", want: "click"},
		{name: "tg link stripped", in: "[open](tg://resolve?domain=foo)", want: "open"},
		{name: "https link kept", in: "[site](https://example.com)", want: `<a href="https://example.com">site</a>`},
		{name: "link with ampersand not double-escaped", in: "[search](https://example.com?a=1&b=2)", want: `<a href="https://example.com?a=1&b=2">search</a>`},
		{name: "link label with html chars", in: "[a < b](https://example.com)", want: `<a href="https://example.com">a &lt; b</a>`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSplitMessage(t *testing.T) {
	longText := strings.Repeat("a", 2500)
	longCode := "```go\n" + strings.Repeat("fmt.Println(\"hello\")\n", 100) + "```" // ~2100 chars

	tests := []struct {
		name         string
		content      string
		maxRunes     int
		expectChunks int
		check        func(t *testing.T, chunks []string)
	}{
		{
			name:         "empty message",
			content:      "",
			maxRunes:     2000,
			expectChunks: 0,
		},
		{
			name:         "short message fits in one chunk",
			content:      "Hello world",
			maxRunes:     2000,
			expectChunks: 1,
		},
		{
			name:         "simple split regular text",
			content:      longText,
			maxRunes:     2000,
			expectChunks: 2,
			check: func(t *testing.T, chunks []string) {
				assert.LessOrEqual(t, len([]rune(chunks[0])), 2000)
				assert.Equal(t, len([]rune(longText)), len([]rune(chunks[0]))+len([]rune(chunks[1])))
			},
		},
		{
			name:         "split at newline",
			content:      strings.Repeat("a", 1750) + "\n" + strings.Repeat("b", 300),
			maxRunes:     2000,
			expectChunks: 2,
			check: func(t *testing.T, chunks []string) {
				assert.Equal(t, 1750, len([]rune(chunks[0])))
				assert.Equal(t, strings.Repeat("b", 300), chunks[1])
			},
		},
		{
			name:         "long code block split",
			content:      "Prefix\n" + longCode,
			maxRunes:     2000,
			expectChunks: 2,
			check: func(t *testing.T, chunks []string) {
				assert.True(t, strings.HasSuffix(chunks[0], "\n```"), "first chunk should end with injected closing fence")
				assert.True(t, strings.HasPrefix(chunks[1], "```go"), "second chunk should start with injected code block header")
			},
		},
		{
			name:         "preserve unicode characters (rune-aware)",
			content:      strings.Repeat("\u4e16", 2500),
			maxRunes:     2000,
			expectChunks: 2,
			check: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					assert.LessOrEqual(t, len([]rune(chunk)), 2000, "chunk %d exceeds maxRunes", i)
					assert.Contains(t, chunk, "\u4e16")
				}
				totalRunes := 0
				for _, chunk := range chunks {
					totalRunes += len([]rune(chunk))
				}
				assert.Equal(t, 2500, totalRunes)
			},
		},
		{
			name:         "zero maxRunes returns single chunk",
			content:      "Hello world",
			maxRunes:     0,
			expectChunks: 1,
			check: func(t *testing.T, chunks []string) {
				assert.Equal(t, "Hello world", chunks[0])
			},
		},
		{
			name:     "all chunks within limit",
			content:  strings.Repeat("word ", 100),
			maxRunes: 50,
			check: func(t *testing.T, chunks []string) {
				for i, c := range chunks {
					assert.LessOrEqual(t, len([]rune(c)), 50, "chunk %d exceeds limit", i)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := splitMessage(tc.content, tc.maxRunes)
			if tc.expectChunks > 0 {
				assert.Len(t, chunks, tc.expectChunks)
			}
			if tc.check != nil {
				tc.check(t, chunks)
			}
		})
	}
}

func TestSplitMessage_CodeBlockIntegrity(t *testing.T) {
	content := "```go\npackage main\n\nfunc main() {\n\tprintln(\"Hello\")\n}\n```"
	maxRunes := 40

	chunks := splitMessage(content, maxRunes)

	if !assert.Len(t, chunks, 2) {
		return
	}

	assert.True(t, strings.HasSuffix(chunks[0], "\n```"), "first chunk should end with closing fence, got: %q", chunks[0])
	assert.True(t, strings.HasPrefix(chunks[1], "```go"), "second chunk should start with code block header, got: %q", chunks[1])
	assert.LessOrEqual(t, len([]rune(chunks[0])), 40, "first chunk exceeded maxRunes")
}

func TestFindLastUnclosedCodeBlockInRange(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		start, end int
		want       int
	}{
		{"no code blocks", "hello world", 0, 11, -1},
		{"complete code block", "```go\ncode\n```", 0, 14, -1},
		{"unclosed code block", "text\n```go\ncode here", 0, 20, 5},
		{"closed then unclosed", "```a\n```\n```b\ncode", 0, 17, 9},
		{"search within subrange", "```a\n```\n```b\ncode", 9, 17, 9},
		{"subrange with no code blocks", "```a\n```\nhello", 9, 14, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.content)
			got := findLastUnclosedCodeBlockInRange(runes, tc.start, tc.end)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindNextClosingCodeBlockInRange(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		startIdx int
		end      int
		want     int
	}{
		{"finds closing fence", "code\n```\nmore", 0, 13, 8},
		{"no closing fence", "just code here", 0, 14, -1},
		{"fence at start of search", "```end", 0, 6, 3},
		{"fence outside range", "code\n```", 0, 4, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.content)
			got := findNextClosingCodeBlockInRange(runes, tc.startIdx, tc.end)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindLastNewlineInRange(t *testing.T) {
	runes := []rune("aaa\nbbb\nccc")

	tests := []struct {
		name         string
		start, end   int
		searchWindow int
		want         int
	}{
		{"finds last newline in full range", 0, 11, 200, 7},
		{"finds newline within search window", 0, 11, 4, 7},
		{"narrow window misses newline outside window", 4, 11, 3, 3},
		{"no newline in range", 0, 3, 200, -1},
		{"range limited to first segment", 0, 4, 200, 3},
		{"search window of 1 at newline", 0, 8, 1, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findLastNewlineInRange(runes, tc.start, tc.end, tc.searchWindow)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindLastSpaceInRange(t *testing.T) {
	runes := []rune("abc def\tghi")

	tests := []struct {
		name         string
		start, end   int
		searchWindow int
		want         int
	}{
		{"finds tab as last space/tab", 0, 11, 200, 7},
		{"finds space when tab out of window", 0, 7, 200, 3},
		{"no space in range", 0, 3, 200, -1},
		{"narrow window finds tab", 5, 11, 4, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findLastSpaceInRange(runes, tc.start, tc.end, tc.searchWindow)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindNewlineFrom(t *testing.T) {
	runes := []rune("hello\nworld\n")

	tests := []struct {
		name string
		from int
		want int
	}{
		{"from start", 0, 5},
		{"from after first newline", 6, 11},
		{"from past all newlines", 12, -1},
		{"from newline itself", 5, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findNewlineFrom(runes, tc.from)
			assert.Equal(t, tc.want, got)
		})
	}
}
