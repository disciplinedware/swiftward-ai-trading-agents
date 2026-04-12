package mcp_handlers

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type grepMatch struct {
	lineNum   int
	lineStart int64
	match     []int // [start, end] relative to file
}

// grep searches for a pattern in a file and returns matches with context.
// Optimized for O(N) time and O(1) memory relative to file size.
func (h *Handlers) grep(filePath string, pattern string, caseSensitive bool, contextLines, maxMatches, widthLimit int) (string, error) {
	if contextLines == 0 {
		contextLines = 5
	}
	if contextLines > 10 {
		contextLines = 10
	}
	if maxMatches == 0 {
		maxMatches = 50
	}
	if maxMatches > 200 {
		maxMatches = 200
	}
	if widthLimit == 0 {
		widthLimit = 1000
	}

	if !caseSensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	const chunkSize = 256 * 1024
	const overlapSize = 4 * 1024
	buf := make([]byte, chunkSize+overlapSize)

	var results []grepMatch

	// State tracking
	var currentLineNum = 1 // 1-based line counting for grep
	var currentLineStart int64 = 0
	var absOffset int64 = 0

	firstChunk := true

	for {
		// Read into buf, leaving room for overlap at start
		// If first chunk, read into buf[0:]
		// If later, read into buf[overlapSize:]

		readStart := 0
		if !firstChunk {
			readStart = overlapSize
		}

		n, err := io.ReadFull(file, buf[readStart:])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return "", err
		}
		if n == 0 {
			break
		}

		validLen := readStart + n
		segment := buf[:validLen]

		// Find matches in this segment
		matches := re.FindAllIndex(segment, -1)

		// Find newlines in this segment
		// We iterate through newlines and matches in parallel to assign line numbers

		matchIdx := 0
		scanPos := 0

		for {
			newlineIdx := bytes.IndexByte(segment[scanPos:], '\n')
			var newlinePos int
			if newlineIdx == -1 {
				newlinePos = validLen // End of segment
			} else {
				newlinePos = scanPos + newlineIdx
			}

			// Process matches that occur before this newline (or end of segment)
			for matchIdx < len(matches) {
				m := matches[matchIdx]
				mStart := m[0]

				if mStart >= newlinePos {
					break // Match is on the next line (or later)
				}

				// Check if we should process this match
				// We only process if it starts in the "new" region (>= overlapSize)
				// OR if it's the first chunk
				shouldProcess := firstChunk || mStart >= overlapSize

				if shouldProcess {
					// We found a match!
					results = append(results, grepMatch{
						lineNum:   currentLineNum,
						lineStart: currentLineStart,
						match:     []int{int(absOffset) + m[0], int(absOffset) + m[1]},
					})

					if len(results) >= maxMatches {
						goto DoneScanning
					}
				}
				matchIdx++
			}

			if newlineIdx == -1 {
				break // No more newlines
			}

			// Advance line counter
			// Only advance if the newline is in the "new" region?
			// No, we must track line numbers continuously across the overlap.
			// BUT we must not double-count lines in the overlap region.
			// Wait, the overlap region is re-scanned.
			// If we re-scan, we re-increment currentLineNum. That's WRONG.

			// Correction:
			// `currentLineNum` must track the line number of the *start of the chunk*.
			// No, that's hard because chunks might split lines.

			// Alternative Strategy:
			// Only increment `currentLineNum` for newlines found in the `new` region.
			// If a newline is in the overlap region, we already counted it in the prev chunk.

			if firstChunk || newlinePos >= overlapSize {
				currentLineNum++
				currentLineStart = absOffset + int64(newlinePos) + 1
			}

			scanPos = newlinePos + 1
		}

		// Prepare for next chunk
		if n < chunkSize {
			break // EOF
		}

		// Move last overlapSize bytes to front
		copy(buf[0:], buf[validLen-overlapSize:validLen])

		// Update absOffset: we advanced by `n` bytes (the amount read)
		// But our buffer logic is:
		// [Overlap][New Data]
		// Next chunk starts after "New Data".
		// "New Data" length is `n` (if !firstChunk) or `n` (if firstChunk? No).

		// If firstChunk: we read `n` bytes. Buffer is [0...n].
		// We copy last overlap.
		// Next read fills [overlap...].
		// absOffset should track the start of the buffer in the file.
		// Actually `absOffset` in my match calc: `int(absOffset) + m[0]`
		// implies `absOffset` is the file offset of `buf[0]`.

		// Start: absOffset = 0.
		// End of Loop 1:
		// We consumed `chunkSize` (roughly).
		// We want next `buf[0]` to be at `chunkSize - overlapSize`.
		// So `absOffset` increases by `validLen - overlapSize`.

		advance := validLen - overlapSize
		absOffset += int64(advance)

		firstChunk = false
	}

DoneScanning:

	if len(results) == 0 {
		return "No matches found.", nil
	}

	// Format results
	var output []string
	for _, m := range results {
		// Read the line content
		// We have lineStart. We need to find where the line ends.
		// We can't use the buffer anymore. We must seek in file.

		file.Seek(m.lineStart, io.SeekStart)
		reader := bufio.NewReader(file)
		line, _ := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n") // Remove delimiter for display

		// Apply Width Limit / Truncation logic
		lineDisplay := line
		lineLen := int64(len(line))

		if lineLen > int64(widthLimit) {
			// Huge line logic
			lineDisplay = truncateMiddleBytesSafe([]byte(line), widthLimit)
		}

		output = append(output, fmt.Sprintf("Match at line %d:", m.lineNum))
		output = append(output, fmt.Sprintf("> %d: %s", m.lineNum, lineDisplay))
		output = append(output, "---")
	}

	if len(results) >= maxMatches {
		output = append(output, "... max matches reached ...")
	}

	return strings.Join(output, "\n"), nil
}
