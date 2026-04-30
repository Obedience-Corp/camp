package crawl

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestDefaultLogAppender_AppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	entries := []LogEntry{
		{ID: "a", Title: "A", From: intent.StatusInbox, Decision: DecisionKeep},
		{ID: "b", Title: "B", From: intent.StatusInbox, Decision: DecisionMove,
			To: intent.StatusReady},
		{ID: "c", Title: "C", From: intent.StatusReady, Decision: DecisionMove,
			To: intent.StatusArchived, Reason: "stale"},
	}

	for _, e := range entries {
		if err := DefaultLogAppender(ctx, dir, e); err != nil {
			t.Fatalf("DefaultLogAppender error = %v", err)
		}
	}

	logPath := CrawlLogPath(dir)
	if !strings.HasSuffix(logPath, "crawl.jsonl") {
		t.Errorf("CrawlLogPath = %q, want suffix crawl.jsonl", logPath)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var got []LogEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e LogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v\nline: %s", err, sc.Text())
		}
		got = append(got, e)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Decision != "keep" || got[1].To != intent.StatusReady || got[2].Reason != "stale" {
		t.Errorf("entries did not round-trip: %+v", got)
	}
	for _, e := range got {
		if e.Timestamp.IsZero() {
			t.Errorf("entry %q timestamp is zero", e.ID)
		}
	}
}

func TestDefaultLogAppender_PreservesProvidedTimestamp(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	when := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	if err := DefaultLogAppender(ctx, dir, LogEntry{
		ID: "x", Title: "X", Decision: DecisionKeep, Timestamp: when,
	}); err != nil {
		t.Fatalf("appender err = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, CrawlLogFile))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var e LogEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !e.Timestamp.Equal(when) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, when)
	}
}

func TestDefaultLogAppender_RespectsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := DefaultLogAppender(ctx, dir, LogEntry{ID: "x"}); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
