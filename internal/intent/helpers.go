package intent

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// priorityRank returns a numeric rank for priority (higher = more urgent).
func priorityRank(p Priority) int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

// isCancelled checks if the user cancelled the edit (empty or unchanged).
func isCancelled(original, modified string) bool {
	if len(bytes.TrimSpace([]byte(modified))) == 0 {
		return true
	}
	return original == modified
}

// moveFile moves a file from src to dst, handling cross-device moves.
func moveFile(src, dst string) error {
	// Try rename first (same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fall back to copy + delete (cross-device)
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst)
		return fmt.Errorf("copying file: %w", err)
	}

	// Ensure data is flushed to disk
	if err := dstFile.Sync(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("syncing destination file: %w", err)
	}

	return os.Remove(src)
}
