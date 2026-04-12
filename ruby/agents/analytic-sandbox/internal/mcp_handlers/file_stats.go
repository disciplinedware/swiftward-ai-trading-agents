package mcp_handlers

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// File header constants
	headerMaxLines     = 10   // Maximum number of lines to show in text file header
	headerMaxTextChars = 2048 // Maximum total characters for text file header
	headerBinaryBytes  = 512  // Number of bytes to show in binary file hexdump
)

// getFileStatistics gathers comprehensive statistics about a file
func (h *Handlers) getFileStatistics(filePath string) (string, error) {
	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}

	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = "(none)"
	}
	sizeBytes := fileInfo.Size()
	sizeKB := float64(sizeBytes) / 1024.0
	sizeMB := sizeKB / 1024.0

	// Detect MIME type
	mimeType := detectMimeType(filePath)
	isBinary := !isTextMime(mimeType)

	var lineCount, maxWidth int
	var avgWidth float64
	var preview string

	// Always get a small hex preview (first 64 bytes) to detect BOMs or binary headers
	hexHead, _ := readBytesAsHex(filePath, 0, 64)
	if hexHead != "" {
		preview += fmt.Sprintf("--- HEX HEAD (First 64 bytes) ---\n%s\n", hexHead)
	}

	if !isBinary {
		// Analyze file lines
		lineCount, maxWidth, avgWidth, err = analyzeFileLines(filePath)
		if err != nil {
			return "", err
		}
		textPreview, err := getTextPreview(filePath, lineCount)
		if err != nil {
			return "", err
		}
		preview += textPreview
	} else {
		// Binary preview (more comprehensive if purely binary)
		binPreview, err := readBytesAsHex(filePath, 0, headerBinaryBytes)
		if err != nil {
			return "", err
		}
		preview += binPreview
		if sizeBytes > headerBinaryBytes {
			preview += fmt.Sprintf("\n... (and %d more bytes)", sizeBytes-headerBinaryBytes)
		}
	}

	// Format output according to API
	result := fmt.Sprintf(`File statistics:
- Original Filename: %s
- File extension: %s
- File type (libmagic): %s
- Is Binary: %v
- File size: %d bytes (%.2f KB / %.2f MB)
`, filename, ext, mimeType, isBinary, sizeBytes, sizeKB, sizeMB)

	if !isBinary {
		result += fmt.Sprintf(`- Line count: %d
- Max line length: %d
- Avg line length: %.0f
`, lineCount, maxWidth, avgWidth)
	}

	result += fmt.Sprintf("\nPREVIEW:\n------\n%s\n------\n", preview)

	return result, nil
}

// detectMimeType uses the file command to detect MIME type
func detectMimeType(filePath string) string {
	cmd := exec.Command("file", "--mime-type", "-b", filePath)
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func isTextMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		strings.Contains(mimeType, "javascript") ||
		strings.Contains(mimeType, "empty") // empty files treated as text usually
}

// analyzeFileLines counts lines and calculates width statistics
func analyzeFileLines(filePath string) (lineCount int, maxWidth int, avgWidth float64, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	totalWidth := 0

	for {
		lineContent, isPrefix, readErr := reader.ReadLine()
		if readErr != nil && readErr != io.EOF {
			return 0, 0, 0, readErr
		}

		if readErr == io.EOF && len(lineContent) == 0 {
			break
		}

		// Calculate full line length
		lineLen := len(lineContent)
		for isPrefix {
			var extra []byte
			extra, isPrefix, readErr = reader.ReadLine()
			if readErr != nil && readErr != io.EOF {
				return 0, 0, 0, readErr
			}
			lineLen += len(extra)
			if readErr == io.EOF {
				break
			}
		}

		lineCount++
		totalWidth += lineLen
		if lineLen > maxWidth {
			maxWidth = lineLen
		}

		if readErr == io.EOF {
			break
		}
	}

	if lineCount > 0 {
		avgWidth = float64(totalWidth) / float64(lineCount)
	}

	return lineCount, maxWidth, avgWidth, nil
}

func getTextPreview(filePath string, totalLines int) (string, error) {
	var sb strings.Builder

	// Head
	sb.WriteString("--- HEAD (First 10 lines) ---\n")
	headLines, _, _ := readLinesFromFile(filePath, 0, 10, 500)
	sb.WriteString(strings.Join(headLines, "\n"))

	if totalLines <= 10 {
		return sb.String(), nil
	}

	// Random Sample (if large enough)
	if totalLines > 30 {
		sb.WriteString("\n\n--- RANDOM SAMPLE (5 lines) ---\n")
		// Pick a random start point between line 10 and total-20
		// Use simple math to pick a middle chunk
		middleStart := 10 + rand.Intn(totalLines-20)
		sampleLines, _, _ := readLinesFromFile(filePath, middleStart, 5, 500)
		for i, line := range sampleLines {
			sb.WriteString(fmt.Sprintf("%d: %s\n", middleStart+i+1, line))
		}
	}

	// Tail
	if totalLines > 10 {
		sb.WriteString("\n\n--- TAIL (Last 10 lines) ---\n")
		startLine := totalLines - 10
		if startLine < 10 {
			startLine = 10
		} // Overlap protection
		tailLines, _, _ := readLinesFromFile(filePath, startLine, 10, 500)
		for i, line := range tailLines {
			sb.WriteString(fmt.Sprintf("%d: %s\n", startLine+i+1, line))
		}
	}

	return sb.String(), nil
}
