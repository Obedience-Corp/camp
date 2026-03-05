package intent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

// AppendDecisionRecord appends a decision record section to an intent's content.
// This is used when moving intents to dungeon statuses to capture the reason.
func AppendDecisionRecord(i *Intent, newStatus Status, reason string) {
	date := time.Now().Format("2006-01-02")
	record := fmt.Sprintf("\n\n## Decision Record\n**Status**: %s | **Date**: %s\n%s", newStatus, date, strings.TrimSpace(reason))
	i.Content += record
	i.UpdatedAt = time.Now()
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
		return camperrors.Wrap(err, "opening source file")
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return camperrors.Wrap(err, "creating destination file")
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst)
		return camperrors.Wrap(err, "copying file")
	}

	// Ensure data is flushed to disk
	if err := dstFile.Sync(); err != nil {
		os.Remove(dst)
		return camperrors.Wrap(err, "syncing destination file")
	}

	return os.Remove(src)
}
