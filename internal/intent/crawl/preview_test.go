package crawl

import (
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestPreviewHeader_AllFieldsRendered(t *testing.T) {
	in := &intent.Intent{
		ID:        "x-20260101",
		Title:     "Test intent",
		Status:    intent.StatusReady,
		Type:      intent.TypeFeature,
		Priority:  intent.PriorityHigh,
		Horizon:   intent.HorizonNext,
		Concept:   "projects/camp",
		UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	got := PreviewHeader(in)
	for _, want := range []string{
		"ID: x-20260101",
		"Status: ready",
		"Type: feature",
		"Priority: high",
		"Horizon: next",
		"Concept: projects/camp",
		"Updated: 2026-04-01",
		"Promoted to: -",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PreviewHeader missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestPreviewHeader_DashesForEmpty(t *testing.T) {
	in := &intent.Intent{ID: "x"}
	got := PreviewHeader(in)
	if !strings.Contains(got, "Type: -") {
		t.Errorf("expected Type: -, got:\n%s", got)
	}
	if !strings.Contains(got, "Priority: -") {
		t.Errorf("expected Priority: -, got:\n%s", got)
	}
	if !strings.Contains(got, "Updated: -") {
		t.Errorf("expected Updated: -, got:\n%s", got)
	}
}

func TestPreviewBody_TrimsAndCapsLines(t *testing.T) {
	in := &intent.Intent{Content: "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"}
	got := PreviewBody(in)
	lines := strings.Split(got, "\n")
	if len(lines) > previewMaxLines {
		t.Errorf("got %d lines, want <= %d", len(lines), previewMaxLines)
	}
}

func TestPreviewBody_CapsChars(t *testing.T) {
	long := strings.Repeat("a", previewMaxChars+200)
	in := &intent.Intent{Content: long}
	got := PreviewBody(in)
	if len(got) > previewMaxChars+3 { // 3 for the "..."
		t.Errorf("got %d chars, want <= %d", len(got), previewMaxChars+3)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected '...' suffix, got %q", got[len(got)-10:])
	}
}

func TestPreviewBody_EmptyForBlankContent(t *testing.T) {
	in := &intent.Intent{Content: "   \n\n  "}
	if got := PreviewBody(in); got != "" {
		t.Errorf("expected empty preview, got %q", got)
	}
}

func TestPreviewDescription_HeaderOnlyWhenNoBody(t *testing.T) {
	in := &intent.Intent{ID: "x", Status: intent.StatusInbox}
	got := PreviewDescription(in)
	if strings.Contains(got, "\n\n") {
		t.Errorf("expected no body section, got:\n%s", got)
	}
}

func TestPreviewTitle_Format(t *testing.T) {
	in := &intent.Intent{Title: "Cleanup intents"}
	got := PreviewTitle(3, 10, in)
	want := "Intent 3/10: Cleanup intents"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreviewTitle_DashWhenNoTitle(t *testing.T) {
	in := &intent.Intent{Title: ""}
	got := PreviewTitle(1, 1, in)
	if !strings.HasSuffix(got, ": -") {
		t.Errorf("expected '-' for empty title, got %q", got)
	}
}
