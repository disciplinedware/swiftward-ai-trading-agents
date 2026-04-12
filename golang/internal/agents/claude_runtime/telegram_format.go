package claude_runtime

import (
	"fmt"
	"regexp"
	"strings"
)

// Telegram HTML formatting regexps (ported from PicoClaw).
var (
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+([^\n]+)`)
	reBlockquote = regexp.MustCompile(`(?m)^>\s*(.*)$`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldStar   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnder  = regexp.MustCompile(`__(.+?)__`)
	reItalic     = regexp.MustCompile(`_([^_]+)_`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reListItem   = regexp.MustCompile(`(?m)^[-*]\s+`)
	reCodeBlock  = regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	reInlineCode = regexp.MustCompile("`([^`]+)`")
)

// markdownToTelegramHTML converts Markdown to Telegram-compatible HTML.
// Code blocks, inline code, and links are extracted first to avoid double-escaping.
func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	// Protect code blocks and inline code from formatting.
	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text

	// Extract links before escapeHTML to avoid double-escaping & in URLs.
	links := extractLinks(text)
	text = links.text

	// Strip heading markers (Telegram has no heading support).
	text = reHeading.ReplaceAllString(text, "$1")
	text = reBlockquote.ReplaceAllString(text, "$1")

	// Escape HTML entities BEFORE applying formatting tags.
	text = escapeHTML(text)

	// Convert markdown to HTML tags.
	text = reBoldStar.ReplaceAllString(text, "<b>$1</b>")
	text = reBoldUnder.ReplaceAllString(text, "<b>$1</b>")
	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		match := reItalic.FindStringSubmatch(s)
		if len(match) < 2 {
			return s
		}
		return "<i>" + match[1] + "</i>"
	})
	text = reStrike.ReplaceAllString(text, "<s>$1</s>")
	text = reListItem.ReplaceAllString(text, "\u2022 ")

	// Restore links (href not double-escaped, label was escaped above).
	for i, link := range links.links {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00LN%d\x00", i), link)
	}

	// Restore protected code with HTML escaping.
	for i, code := range inlineCodes.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", escaped))
	}
	for i, code := range codeBlocks.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", escaped))
	}

	return text
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	matches := reCodeBlock.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}
	i := 0
	text = reCodeBlock.ReplaceAllStringFunc(text, func(_ string) string {
		ph := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return ph
	})
	return codeBlockMatch{text: text, codes: codes}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	matches := reInlineCode.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}
	i := 0
	text = reInlineCode.ReplaceAllStringFunc(text, func(_ string) string {
		ph := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return ph
	})
	return inlineCodeMatch{text: text, codes: codes}
}

type linkMatch struct {
	text  string
	links []string // pre-rendered HTML <a> tags or plain label
}

// extractLinks replaces markdown links with placeholders and renders them to HTML.
// Called before escapeHTML so that URLs with & are not double-escaped.
func extractLinks(text string) linkMatch {
	matches := reLink.FindAllStringSubmatch(text, -1)
	rendered := make([]string, 0, len(matches))
	for _, m := range matches {
		label := escapeHTML(m[1])
		href := m[2]
		if strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://") {
			rendered = append(rendered, fmt.Sprintf(`<a href="%s">%s</a>`, href, label))
		} else {
			// Non-http links (javascript:, tg://, file refs) - render as plain text.
			rendered = append(rendered, label)
		}
	}
	i := 0
	text = reLink.ReplaceAllStringFunc(text, func(_ string) string {
		ph := fmt.Sprintf("\x00LN%d\x00", i)
		i++
		return ph
	})
	return linkMatch{text: text, links: rendered}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// splitMessage splits long messages into chunks that fit within maxRunes,
// preserving code block integrity. Ported from PicoClaw's SplitMessage.
// The function reserves a buffer (10% of maxRunes, min 50) to leave room for closing code blocks.
func splitMessage(content string, maxRunes int) []string {
	if maxRunes <= 0 {
		if content == "" {
			return nil
		}
		return []string{content}
	}

	runes := []rune(content)
	totalLen := len(runes)
	var messages []string

	// Dynamic buffer: 10% of maxRunes, but at least 50 chars if possible.
	codeBlockBuffer := max(maxRunes/10, 50)
	if codeBlockBuffer > maxRunes/2 {
		codeBlockBuffer = maxRunes / 2
	}

	start := 0
	for start < totalLen {
		remaining := totalLen - start
		if remaining <= maxRunes {
			messages = append(messages, string(runes[start:totalLen]))
			break
		}

		// Effective split point: maxRunes minus buffer, to leave room for code blocks.
		effectiveLimit := max(maxRunes-codeBlockBuffer, maxRunes/2)
		end := start + effectiveLimit

		// Find natural split point within the effective limit.
		msgEnd := findLastNewlineInRange(runes, start, end, 200)
		if msgEnd <= start {
			msgEnd = findLastSpaceInRange(runes, start, end, 100)
		}
		if msgEnd <= start {
			msgEnd = end
		}

		// Check if this would end with an incomplete code block.
		unclosedIdx := findLastUnclosedCodeBlockInRange(runes, start, msgEnd)

		if unclosedIdx >= 0 {
			// Message would end with incomplete code block.
			// Try to extend up to maxRunes to include the closing ```.
			if totalLen > msgEnd {
				closingIdx := findNextClosingCodeBlockInRange(runes, msgEnd, totalLen)
				if closingIdx > 0 && closingIdx-start <= maxRunes {
					msgEnd = closingIdx
				} else {
					// Code block is too long to fit in one chunk or missing closing fence.
					// Try to split inside by injecting closing and reopening fences.
					headerEnd := findNewlineFrom(runes, unclosedIdx)
					var header string
					if headerEnd == -1 {
						header = strings.TrimSpace(string(runes[unclosedIdx : unclosedIdx+3]))
					} else {
						header = strings.TrimSpace(string(runes[unclosedIdx:headerEnd]))
					}
					headerEndIdx := unclosedIdx + len([]rune(header))
					if headerEnd != -1 {
						headerEndIdx = headerEnd
					}

					// If we have a reasonable amount of content after the header, split inside.
					if msgEnd > headerEndIdx+20 {
						innerLimit := min(start+maxRunes-5, totalLen) // leave room for "\n```"
						betterEnd := findLastNewlineInRange(runes, start, innerLimit, 200)
						if betterEnd > headerEndIdx {
							msgEnd = betterEnd
						} else {
							msgEnd = innerLimit
						}
						chunk := strings.TrimRight(string(runes[start:msgEnd]), " \t\n\r") + "\n```"
						messages = append(messages, chunk)
						remaining := strings.TrimSpace(header + "\n" + string(runes[msgEnd:totalLen]))
						runes = []rune(remaining)
						totalLen = len(runes)
						start = 0
						continue
					}

					// Otherwise, try to split before the code block starts.
					newEnd := findLastNewlineInRange(runes, start, unclosedIdx, 200)
					if newEnd <= start {
						newEnd = findLastSpaceInRange(runes, start, unclosedIdx, 100)
					}
					if newEnd > start {
						msgEnd = newEnd
					} else {
						if unclosedIdx-start > 20 {
							msgEnd = unclosedIdx
						} else {
							splitAt := min(start+maxRunes-5, totalLen)
							chunk := strings.TrimRight(string(runes[start:splitAt]), " \t\n\r") + "\n```"
							messages = append(messages, chunk)
							remaining := strings.TrimSpace(header + "\n" + string(runes[splitAt:totalLen]))
							runes = []rune(remaining)
							totalLen = len(runes)
							start = 0
							continue
						}
					}
				}
			}
		}

		if msgEnd <= start {
			msgEnd = start + effectiveLimit
		}

		messages = append(messages, string(runes[start:msgEnd]))
		// Advance start, skipping leading whitespace of next chunk.
		start = msgEnd
		for start < totalLen && (runes[start] == ' ' || runes[start] == '\t' || runes[start] == '\n' || runes[start] == '\r') {
			start++
		}
	}

	return messages
}

// findLastUnclosedCodeBlockInRange finds the last opening ``` that doesn't have a closing ```
// within runes[start:end]. Returns the absolute rune index or -1.
func findLastUnclosedCodeBlockInRange(runes []rune, start, end int) int {
	inCodeBlock := false
	lastOpenIdx := -1

	for i := start; i < end; i++ {
		if i+2 < end && runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			if !inCodeBlock {
				lastOpenIdx = i
			}
			inCodeBlock = !inCodeBlock
			i += 2
		}
	}

	if inCodeBlock {
		return lastOpenIdx
	}
	return -1
}

// findNextClosingCodeBlockInRange finds the next closing ``` starting from startIdx
// within runes[startIdx:end]. Returns the absolute index after the closing ``` or -1.
func findNextClosingCodeBlockInRange(runes []rune, startIdx, end int) int {
	for i := startIdx; i < end; i++ {
		if i+2 < end && runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			return i + 3
		}
	}
	return -1
}

// findNewlineFrom finds the first newline character starting from the given index.
func findNewlineFrom(runes []rune, from int) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == '\n' {
			return i
		}
	}
	return -1
}

// findLastNewlineInRange finds the last newline within the last searchWindow runes
// of the range runes[start:end]. Returns the absolute index or start-1 (indicating not found).
func findLastNewlineInRange(runes []rune, start, end, searchWindow int) int {
	searchStart := max(end-searchWindow, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == '\n' {
			return i
		}
	}
	return start - 1
}

// findLastSpaceInRange finds the last space/tab within the last searchWindow runes
// of the range runes[start:end]. Returns the absolute index or start-1 (indicating not found).
func findLastSpaceInRange(runes []rune, start, end, searchWindow int) int {
	searchStart := max(end-searchWindow, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i
		}
	}
	return start - 1
}
