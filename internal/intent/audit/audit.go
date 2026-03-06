// Package audit provides an append-only event log for intent lifecycle events.
//
// Events are written as JSONL to .intents.jsonl in the intents directory,
// providing a complete audit trail of all status transitions, promotions,
// and other intent operations.
package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AuditFile is the name of the append-only JSONL audit log in the intents directory.
const AuditFile = ".intents.jsonl"

// FilePath returns the full path to the audit log file within the given intents directory.
func FilePath(intentsDir string) string {
	return filepath.Join(intentsDir, AuditFile)
}

// EventType identifies the kind of audit event.
type EventType string

const (
	EventCreate  EventType = "create"
	EventMove    EventType = "move"
	EventPromote EventType = "promote"
	EventArchive EventType = "archive"
	EventDelete  EventType = "delete"
	EventGather  EventType = "gather"
)

// Event represents a single audit trail entry.
type Event struct {
	Timestamp  time.Time `json:"ts"`
	Type       EventType `json:"event"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	PromotedTo string    `json:"promoted_to,omitempty"`
	Actor      string    `json:"actor,omitempty"`
}

// AppendEvent writes an audit event to the JSONL log file.
// The intentsDir is the base intents directory (e.g. workflow/intents/).
// If the timestamp is zero, it defaults to now.
func AppendEvent(ctx context.Context, intentsDir string, e Event) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled before writing audit event")
	}

	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(e)
	if err != nil {
		return camperrors.Wrap(err, "marshaling audit event")
	}
	data = append(data, '\n')

	path := FilePath(intentsDir)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return camperrors.Wrap(err, "opening audit log")
	}
	defer f.Close()

	_, err = f.Write(data)
	if err != nil {
		return camperrors.Wrap(err, "writing audit event")
	}
	return nil
}
