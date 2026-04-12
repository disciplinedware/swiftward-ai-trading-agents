package mcp_handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// validateSyntax performs syntax validation for supported file types (.py, .go, .rb)
// by running validation commands inside the session's Docker container.
func (h *Handlers) validateSyntax(ctx context.Context, hostPath string, content string) error {
	ext := strings.ToLower(filepath.Ext(hostPath))
	var cmd []string
	var displayType string

	// Create a temporary file in the workspace to validate
	// We use a random suffix to avoid collisions
	// We use a prefix that won't be ignored by go build (dot/underscore are ignored)
	tempFileName := fmt.Sprintf("tmp_validate_%d%s", time.Now().UnixNano(), ext)
	tempHostPath := filepath.Join(filepath.Dir(hostPath), tempFileName)

	// Ensure we use the proper path in the container relative to /app
	relPath, err := filepath.Rel(h.sess.HostWorkDir, tempHostPath)
	if err != nil {
		// fall back to default if something is weird
		relPath = filepath.Base(tempHostPath)
	}
	tempInContainer := filepath.Join("/app", relPath)

	switch ext {
	case ".py":
		cmd = []string{"python3", "-m", "py_compile", tempInContainer}
		displayType = "Python"
	case ".go":
		cmd = []string{"go", "build", "-o", "/dev/null", tempInContainer}
		displayType = "Go"
	case ".rb":
		cmd = []string{"ruby", "-c", tempInContainer}
		displayType = "Ruby"
	default:
		// No validation for other file types
		return nil
	}

	// Write content to temp file on host (shared with container)
	if err := os.WriteFile(tempHostPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write temporary validation file: %w", err)
	}
	defer os.Remove(tempHostPath)

	// No lock here - caller (handleWriteFile/handleEditFile) already holds h.sess.ExecMutex

	execRes, err := h.dm.Exec(ctx, h.sess.ContainerID, cmd, 30*time.Second, "")
	if err != nil {
		return fmt.Errorf("syntax validation failed to execute: %w", err)
	}

	if execRes.ExitCode != 0 {
		errorDetail := execRes.Stderr
		if errorDetail == "" {
			errorDetail = execRes.Stdout
		}
		// Clean up the error message by replacing the temp filename with the actual filename or a generic placeholder
		errorDetail = strings.ReplaceAll(errorDetail, tempInContainer, filepath.Join("/app", filepath.Base(hostPath)))
		errorDetail = strings.ReplaceAll(errorDetail, tempFileName, filepath.Base(hostPath))

		return fmt.Errorf("%s syntax validation FAILED. Changes were NOT applied.\n\nError output:\n%s", displayType, errorDetail)
	}

	return nil
}
