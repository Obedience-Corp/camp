// Package audit appends promote (and future) events to a campaign-level
// workitem audit log, parallel to the intent audit log.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const AuditFile = ".workitems.jsonl"

type EventType string

const (
	EventPromote EventType = "promote"
	EventGather  EventType = "gather"
	EventCreate  EventType = "create"
	EventAdopt   EventType = "adopt"
	// EventMove records a directory-workitem relocation that is not itself a
	// promote (e.g. `camp dungeon move` triage or status changes). Mirrors
	// the intent audit log's own "move" event for cross-ledger consistency.
	EventMove EventType = "move"
)

type Event struct {
	Timestamp    time.Time `json:"ts"`
	Event        EventType `json:"event"`
	ID           string    `json:"id"`
	Ref          string    `json:"ref,omitempty"`
	Type         string    `json:"type,omitempty"`
	Title        string    `json:"title,omitempty"`
	From         string    `json:"from,omitempty"`
	To           string    `json:"to,omitempty"`
	Target       string    `json:"target,omitempty"`
	PromotedTo   string    `json:"promoted_to,omitempty"`
	GatheredInto string    `json:"gathered_into,omitempty"`
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

// AppendBestEffort is the single shared entry point every FS-mutating
// workitem command routes through to record a ledger event. A ledger write
// failure never fails the caller's already-applied filesystem mutation; it
// is reported to warn instead, so degraded ledger coverage is visible
// without stranding an operator behind a spurious command failure.
func AppendBestEffort(ctx context.Context, warn io.Writer, campaignRoot string, e Event) {
	if err := AppendEvent(ctx, campaignRoot, e); err != nil {
		_, _ = fmt.Fprintf(warn, "warning: failed to append workitem audit event: %v\n", err)
	}
}
