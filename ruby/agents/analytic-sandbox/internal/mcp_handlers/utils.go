package mcp_handlers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// readLinesFromFile is a shared helper to read a range of lines from a file with a width limit per line.
// It is memory-efficient and can handle lines of any length (e.g. 1MB SQL dumps).
// It returns the lines, a boolean indicating if there are more lines in the file, and any error.
func readLinesFromFile(filePath string, offset, count, widthLimit int) ([]string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var lines []string
	lineNum := 0

	for {
		if lineNum >= offset+count {
			// Check if there is at least one more byte to read
			_, _, err := reader.ReadLine()
			if err == nil {
				return lines, true, nil // There is more content
			}
			if err == io.EOF {
				return lines, false, nil // End of file reached exactly
			}
			// If error is not EOF, it's a read error, but we have some lines.
			// Let's return what we have and the error? Or just treat as EOF for purposes of "more" check?
			// The original loop would break here.
			// Let's just return false for hasMore if we can't read more.
			return lines, false, nil
		}

		// ReadLine reads until \n or the end of the buffer.
		// If the line is longer than the buffer, it returns isPrefix=true.
		lineContent, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, false, err
		}

		if lineNum >= offset {
			// First, read the entire line to know its true length
			fullLineContent := lineContent
			totalInLine := len(lineContent)
			tempIsPrefix := isPrefix

			// If the line continues beyond the buffer, read the rest
			for tempIsPrefix {
				var extra []byte
				extra, tempIsPrefix, err = reader.ReadLine()
				if err != nil && err != io.EOF {
					return nil, false, err
				}
				fullLineContent = append(fullLineContent, extra...)
				totalInLine += len(extra)
				if err == io.EOF {
					break
				}
			}

			// Now determine what to display
			var displayLine string

			if totalInLine > widthLimit {
				// Use middle truncation
				displayLine = truncateMiddleBytesSafe(fullLineContent, widthLimit)
			} else {
				displayLine = string(fullLineContent)
			}
			lines = append(lines, displayLine)
		} else {
			// Just skip the line content
			for isPrefix {
				_, isPrefix, err = reader.ReadLine()
				if err != nil && err != io.EOF {
					return nil, false, err
				}
				if err == io.EOF {
					break
				}
			}
		}
		lineNum++
	}

	return lines, false, nil
}

// readBytesAsHex reads a chunk of bytes and returns a hexdump string
func readBytesAsHex(filePath string, offset, count int64) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}

	buf := make([]byte, count)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	buf = buf[:n]

	var result strings.Builder
	for i := 0; i < n; i += 16 {
		end := i + 16
		if end > n {
			end = n
		}

		// Offset
		result.WriteString(fmt.Sprintf("%08x  ", offset+int64(i)))

		// Hex bytes
		for j := i; j < i+16; j++ {
			if j < end {
				result.WriteString(fmt.Sprintf("%02x ", buf[j]))
			} else {
				result.WriteString("   ")
			}
			if j == i+7 {
				result.WriteString(" ")
			}
		}

		// ASCII representation
		result.WriteString(" |")
		for j := i; j < end; j++ {
			if buf[j] >= 32 && buf[j] <= 126 {
				result.WriteByte(buf[j])
			} else {
				result.WriteByte('.')
			}
		}
		result.WriteString("|\n")
	}

	return result.String(), nil
}

// truncateBytesSafe truncates a byte slice to at most maxBytes,
// ensuring that the cut does not split a UTF-8 character.
// It returns the truncated string and the number of bytes skipped.
func truncateBytesSafe(b []byte, maxBytes int) (string, int) {
	if len(b) <= maxBytes {
		return string(b), 0
	}

	cut := maxBytes
	// While we are within the slice and sitting on a continuation byte (10xxxxxx), back off.
	for cut > 0 && cut < len(b) && (b[cut]&0xC0 == 0x80) {
		cut--
	}
	return string(b[:cut]), len(b) - cut
}

// truncateMiddleBytesSafe truncates the middle of a byte slice if it exceeds maxBytes.
func truncateMiddleBytesSafe(b []byte, maxBytes int) string {
	if len(b) <= maxBytes {
		return string(b)
	}

	// Calculate sizes for start and end parts
	// We want to show something like: "Start... (skipped N bytes) ...End"
	// Let's allocate 30% to the end and 70% to the start, minus some overhead for the message.

	const msgTemplate = "... (skipped %d bytes) ..."
	// A rough estimate of message length: "... (skipped 1000000 bytes) ..." is ~30 chars.
	// Let's reserve 40 bytes for safety.
	reserved := 40
	if maxBytes <= reserved {
		// Fallback to simple tail truncation if width is too small
		truncated, skipped := truncateBytesSafe(b, maxBytes)
		if skipped > 0 {
			return fmt.Sprintf("%s... (skipped %d bytes)", truncated, skipped)
		}
		return truncated
	}

	displayTotal := maxBytes - reserved
	endSize := displayTotal / 3
	startSize := displayTotal - endSize

	// Start part
	startPart, _ := truncateBytesSafe(b, startSize)

	// End part
	// We need to cut 'endSize' bytes from the end, safely.
	endOffset := len(b) - endSize
	for endOffset < len(b) && (b[endOffset]&0xC0 == 0x80) {
		endOffset++
	}
	endPart := string(b[endOffset:])
	skipped := len(b) - len(startPart) - len(endPart)

	return fmt.Sprintf("%s... (skipped %d bytes) ...%s", startPart, skipped, endPart)
}
