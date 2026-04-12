package mcp_handlers

import (
	"fmt"
	"strings"
)

// getLines retrieves lines from the specified file path using native Go types.
func (h *Handlers) getLines(filePath string, offset, count, widthLimit int) (string, error) {
	// If count was not provided, default to a safe small number
	// However, we expect the caller (handleReadFile) to have set a language-specific default already
	// if count was 0.
	if count == 0 {
		count = 20
	}
	// We might want to still have SOME upper bound to prevent abuse, but 300 is fine.
	if count > 2000 {
		count = 2000
	}
	if widthLimit == 0 {
		widthLimit = 1000
	}

	lines, hasMore, err := readLinesFromFile(filePath, offset, count, widthLimit)
	if err != nil {
		return "", err
	}

	var formattedLines []string

	if offset > 0 {
		formattedLines = append(formattedLines, fmt.Sprintf("[SYSTEM] Reading file starting from line %d. Lines before this offset are omitted.", offset))
	}

	for i, line := range lines {
		formattedLines = append(formattedLines, fmt.Sprintf("%d: %s", offset+i, line))
	}

	if hasMore {
		formattedLines = append(formattedLines, fmt.Sprintf("[SYSTEM] Returned %d lines due to count limit. There is additional content below.", count))
	}

	return strings.Join(formattedLines, "\n"), nil
}
