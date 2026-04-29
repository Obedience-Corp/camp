package crawl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// CrawlLogFile is the JSONL log written for every intent crawl
// decision (keep, skip, and move). It lives alongside the
// .intents.jsonl audit log so all intent history is colocated.
const CrawlLogFile = "crawl.jsonl"

// CrawlLogPath returns the absolute path to the crawl log inside
// intentsDir.
func CrawlLogPath(intentsDir string) string {
	return filepath.Join(intentsDir, CrawlLogFile)
}

// LogEntry is one JSONL row in the intent crawl log. Reason is
// only set for moves; To is only set for moves.
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	From      intent.Status  `json:"from"`
	Decision  string         `json:"decision"` // keep | skip | move
	To        intent.Status  `json:"to,omitempty"`
	Reason    string         `json:"reason,omitempty"`
}

// LogAppender appends one LogEntry to the crawl log. It is a
// function type to keep the runner test seam minimal: tests pass
// in a closure that captures entries in a slice.
type LogAppender func(ctx context.Context, intentsDir string, entry LogEntry) error

// DefaultLogAppender writes the entry as a JSON line to
// CrawlLogPath(intentsDir). The file is created with 0644 if it
// does not exist.
func DefaultLogAppender(ctx context.Context, intentsDir string, entry LogEntry) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled before crawl log write")
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return camperrors.Wrap(err, "marshaling crawl log entry")
	}
	data = append(data, '\n')

	path := CrawlLogPath(intentsDir)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return camperrors.Wrap(err, "opening crawl log")
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return camperrors.Wrap(err, "writing crawl log entry")
	}
	return nil
}
