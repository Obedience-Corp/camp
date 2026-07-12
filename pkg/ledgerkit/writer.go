package ledgerkit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// EventsDir is the ledger root under the campaign directory (D002).
const EventsDir = ".campaign/events"

// shardOpener opens a shard path for appending. Injected so the writer's
// routing/encoding/error logic is unit-testable without host filesystem
// mutation; the default (osAppend) is the only piece that touches disk and is
// exercised on real disk by CLI-level integration once commands emit (seq 03+).
type shardOpener func(path string) (io.WriteCloser, error)

// Writer appends events to a single machine/actor's monthly shard. It is
// constructed with an explicit writer id (dependency injection) so the core is
// testable without global config; camp/fest resolve the machine id via
// ResolveWriterID.
type Writer struct {
	campaignRoot string
	writerID     string
	open         shardOpener
}

// NewWriter returns a Writer for the campaign at campaignRoot writing as
// writerID (a stable per-machine slug; see ResolveWriterID). Both must be
// non-empty.
func NewWriter(campaignRoot, writerID string) (*Writer, error) {
	if campaignRoot == "" {
		return nil, camperrors.New("ledgerkit: empty campaign root")
	}
	if writerID == "" {
		return nil, camperrors.New("ledgerkit: empty writer id")
	}
	return &Writer{campaignRoot: campaignRoot, writerID: writerID, open: osAppend}, nil
}

// osAppend is the default shard opener: it creates the month directory and
// opens the shard for a single O_APPEND write.
func osAppend(path string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, camperrors.Wrapf(err, "ledgerkit: create shard dir for %s", path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, camperrors.Wrapf(err, "ledgerkit: open shard %s", path)
	}
	return f, nil
}

// ShardPath returns the shard file an event with timestamp ts belongs in:
// <root>/.campaign/events/<YYYY-MM>/<writer>.jsonl. Two writers never share a
// file in a month, so cross-writer merges are file-adds git resolves without
// conflict (D002). It is a pure function for testability.
func ShardPath(campaignRoot, writerID string, ts time.Time) string {
	month := ts.UTC().Format("2006-01")
	return filepath.Join(campaignRoot, EventsDir, month, writerID+".jsonl")
}

// encodeEvent marshals an event to a single newline-terminated JSON line. It
// rejects a payload that cannot be marshaled so a bad event never corrupts the
// shard with a partial line.
func encodeEvent(ev *Event) ([]byte, error) {
	line, err := json.Marshal(ev)
	if err != nil {
		return nil, camperrors.Wrapf(err, "ledgerkit: marshal event %s", ev.ID)
	}
	return append(line, '\n'), nil
}

// shardTime derives the month bucket from the event's own ts, falling back to
// now (UTC) when ts is absent or unparseable so an event is never dropped.
func shardTime(ev *Event) time.Time {
	if ev.TS != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.TS); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}

// Append writes one event as a single atomic O_APPEND line to the event's
// monthly shard, creating the month directory if needed. The single write of a
// newline-terminated line is the atomicity boundary; there is no lock beyond
// the OS append guarantee (D002/D003). It returns an error on failure; callers
// on a state-change hot path use Emit, which makes failure loud-but-not-fatal.
func (w *Writer) Append(ctx context.Context, ev *Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ev == nil {
		return camperrors.New("ledgerkit: nil event")
	}
	if ev.ID == "" {
		return camperrors.New("ledgerkit: event has no id")
	}
	line, err := encodeEvent(ev)
	if err != nil {
		return err
	}
	path := ShardPath(w.campaignRoot, w.writerID, shardTime(ev))
	f, err := w.open(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return camperrors.Wrapf(err, "ledgerkit: append to shard %s", path)
	}
	if err := f.Close(); err != nil {
		return camperrors.Wrapf(err, "ledgerkit: close shard %s", path)
	}
	return nil
}

// Emit appends an event best-effort (D003): a failed append never blocks the
// caller's state change. On failure it invokes warn (for a loud CLI warning)
// and returns the error for optional inspection, but callers ignore the return
// for control flow. This is the emission path every state-changing command
// uses, so a ledger problem is visible yet never breaks the user's action.
func (w *Writer) Emit(ctx context.Context, ev *Event, warn func(error)) error {
	err := w.Append(ctx, ev)
	if err != nil && warn != nil {
		warn(err)
	}
	return err
}
