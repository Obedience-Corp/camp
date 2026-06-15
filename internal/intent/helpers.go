package intent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
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

// moveFile moves src to dst with no-replace semantics.
// The pre-rename stat guard reduces, but does not eliminate, the file
// destination TOCTOU window before os.Rename.
func moveFile(src, dst string) error {
	if err := ensureMoveDestinationAvailable(dst); err != nil {
		return err
	}

	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isExdevError(err) {
		return camperrors.Wrap(err, "renaming file")
	}

	return copyFileNoReplace(src, dst)
}

func ensureMoveDestinationAvailable(dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return camperrors.Wrap(ErrFileExists, dst)
	} else if !os.IsNotExist(err) {
		return camperrors.Wrap(err, "checking destination")
	}
	return nil
}

func copyFileNoReplace(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return camperrors.Wrap(err, "opening source file")
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return camperrors.Wrap(ErrFileExists, dst)
		}
		return camperrors.Wrap(err, "creating destination file")
	}

	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return camperrors.Wrap(err, "copying file")
	}

	if err := dstFile.Sync(); err != nil {
		_ = dstFile.Close()
		return camperrors.Wrap(err, "syncing destination file")
	}

	if err := dstFile.Close(); err != nil {
		return camperrors.Wrap(err, "closing destination file")
	}

	if err := os.Remove(src); err != nil {
		return camperrors.Wrap(err, "removing source file")
	}

	removeOnFailure = false
	return nil
}

func isExdevError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}
