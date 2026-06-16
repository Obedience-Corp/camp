package intent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/statusmove"
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

// moveFile moves src to dst with shared no-replace semantics.
func moveFile(src, dst string) error {
	if _, err := statusmove.Move(context.Background(), src, dst, statusmove.MoveOptions{}); err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return camperrors.Wrap(ErrFileExists, dst)
		}
		return err
	}
	return nil
}
