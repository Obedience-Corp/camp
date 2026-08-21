package triage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/triage"
)

func TestWriteStatusText_PrintsStaleNotice(t *testing.T) {
	status := &triage.Status{
		RunID:   "run-1",
		Mode:    triage.RunModeIncremental,
		Profile: "default",
		Phase:   triage.PhaseSnapshotted,
		Active:  true,
		Rows:    1,
		Counts:  map[string]int{"pending-evidence": 1},
	}
	const notice = "last triage was 20 days ago — run: camp triage start"
	var buf bytes.Buffer
	if err := writeStatusText(&buf, status, notice); err != nil {
		t.Fatalf("writeStatusText: %v", err)
	}
	if !strings.Contains(buf.String(), notice) {
		t.Fatalf("status text missing stale notice:\n%s", buf.String())
	}
}

func TestWriteStatusText_OmitsEmptyNotice(t *testing.T) {
	status := &triage.Status{
		RunID:  "run-1",
		Active: true,
		Counts: map[string]int{},
	}
	var buf bytes.Buffer
	if err := writeStatusText(&buf, status, ""); err != nil {
		t.Fatalf("writeStatusText: %v", err)
	}
	if strings.Contains(buf.String(), "camp triage start") {
		t.Fatalf("empty notice must not appear:\n%s", buf.String())
	}
}
