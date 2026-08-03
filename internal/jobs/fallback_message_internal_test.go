package jobs

import (
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// errNotAWriterFailure stands in for any error that did not come from the
// configured writer.
var errNotAWriterFailure = errors.New("camp could not materialize the tree")

// The fallback subject is permanent. It is written when the configured writer
// is unavailable, and unlike a queue file it stays in git history for as long
// as the repository exists, so its shape is a contract with the user rather
// than an implementation detail.
func TestFallbackSubjectFor(t *testing.T) {
	t.Parallel()

	longPath := "projects/camp/internal/" + strings.Repeat("deep/", 12) + "file.go"

	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			// git could not say what changed. The commit still lands, so it
			// still needs a subject.
			name:  "no paths",
			paths: nil,
			want:  "Deferred commit (writer unavailable)",
		},
		{
			name:  "one path is named outright",
			paths: []string{".campaign/intents/inbox/idea.md"},
			want:  "Update .campaign/intents/inbox/idea.md (writer unavailable)",
		},
		{
			// Truncating would produce something that reads like a path and is
			// not one, which is worse than saying less.
			name:  "a path too long for a subject degrades to the count",
			paths: []string{longPath},
			want:  "Update 1 file (writer unavailable)",
		},
		{
			name:  "several paths become a count",
			paths: []string{"a.md", "b.md", "c.md"},
			want:  "Update 3 files (writer unavailable)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fallbackSubjectFor(tt.paths)
			if got != tt.want {
				t.Fatalf("fallbackSubjectFor() = %q, want %q", got, tt.want)
			}
			if !strings.Contains(got, FallbackSubjectMarker) {
				t.Fatalf("subject %q must carry the marker that makes these commits findable", got)
			}
		})
	}
}

// Every generated subject stays inside the budget, so the campaign tag camp
// prepends afterwards cannot push the subject line past what git log shows.
func TestFallbackSubjectForStaysWithinBudget(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		nil,
		{"short.md"},
		{strings.Repeat("x/", 200) + "file.go"},
		{"a", "b", "c", "d", "e"},
	}
	for _, paths := range cases {
		if got := fallbackSubjectFor(paths); len(got) > maxFallbackSubject {
			t.Fatalf("fallbackSubjectFor(%v) = %q, length %d exceeds the %d budget",
				paths, got, len(got), maxFallbackSubject)
		}
	}
}

// Only a writer's own diagnostic is repeated into a commit message. Camp's
// internal error text is not, because it describes camp's plumbing rather than
// anything the user configured or can act on.
func TestWriterFailureReasonOnlyReportsTheWriter(t *testing.T) {
	t.Parallel()

	writerErr := &autowrite.WriterError{Reason: "daemon not running"}
	if got := writerFailureReason(writerErr); got != "daemon not running" {
		t.Fatalf("writerFailureReason(WriterError) = %q, want the writer's reason", got)
	}

	if got := writerFailureReason(errNotAWriterFailure); got != "" {
		t.Fatalf("writerFailureReason(other) = %q, want empty", got)
	}

	if got := writerFailureReason(nil); got != "" {
		t.Fatalf("writerFailureReason(nil) = %q, want empty", got)
	}
}
