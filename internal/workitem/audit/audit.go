// Package audit appends promote (and future) events to a campaign-level
// workitem audit log, parallel to the intent audit log.
package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const AuditFile = ".workitems.jsonl"

type EventType string

const EventPromote EventType = "promote"

type Event struct {
	Timestamp  time.Time `json:"ts"`
	Event      EventType `json:"event"`
	ID         string    `json:"id"`
	Type       string    `json:"type,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Target     string    `json:"target,omitempty"`
	PromotedTo string    `json:"promoted_to,omitempty"`
}

func AppendEvent(ctx context.Context, campaignRoot string, e Event) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled before writing audit event")
	}

	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	dir := filepath.Join(campaignRoot, ".campaign", "workitems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return camperrors.Wrap(err, "creating workitem audit directory")
	}

	data, err := json.Marshal(e)
	if err != nil {
		return camperrors.Wrap(err, "marshaling audit event")
	}
	data = append(data, '\n')

	f, err := os.OpenFile(filepath.Join(dir, AuditFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return camperrors.Wrap(err, "opening workitem audit log")
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return camperrors.Wrap(err, "writing workitem audit event")
	}
	return nil
}
