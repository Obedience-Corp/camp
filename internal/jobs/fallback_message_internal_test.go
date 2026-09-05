package jobs

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// errNotAWriterFailure stands in for any error that did not come from the
// configured writer.
var errNotAWriterFailure = errors.New("camp could not materialize the tree")

// The property the whole fallback exists for: a writer that cannot answer
// costs the user a subject, never a commit. Every failure shape the writer can
// produce has to come back with a message and no error, because the caller
// treats an error here as a parked job.
func TestMessageForTreeNeverFailsAnAutoWriteJob(t *testing.T) {
	tests := []struct {
		name         string
		writerOut    string
		writerErr    error
		prefix       string
		wantFallback bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "writer reports its own outage",
			writerErr:    &autowrite.WriterError{Reason: "daemon not running"},
			wantFallback: true,
			wantContains: []string{FallbackSubjectMarker, "daemon not running", "git commit --amend"},
		},
		{
			// The transient provider failure that lost three commits on
			// 2026-09-01. A cold model is not a reason to drop a commit.
			name:         "writer reports a transient provider failure",
			writerErr:    &autowrite.WriterError{Reason: "transient provider failure: warm ollama model"},
			wantFallback: true,
			wantContains: []string{FallbackSubjectMarker, "warm ollama model"},
		},
		{
			// Camp's own plumbing errors are not repeated into git history:
			// they describe camp, not anything the user configured.
			name:         "camp's own failure is not quoted into the commit",
			writerErr:    errNotAWriterFailure,
			wantFallback: true,
			wantContains: []string{FallbackSubjectMarker},
			wantAbsent:   []string{"materialize the tree"},
		},
		{
			// Exit zero with no output leaves the commit just as unlabelled as
			// a crash does, and the commit is owed either way.
			name:         "writer exits clean and prints nothing",
			writerOut:    "   \n",
			wantFallback: true,
			wantContains: []string{FallbackSubjectMarker},
		},
		{
			name:         "a working writer is left alone",
			writerOut:    "feat: the writer's own subject",
			wantContains: []string{"feat: the writer's own subject"},
			wantAbsent:   []string{FallbackSubjectMarker},
		},
		{
			// The campaign tag is the enqueuer's, not the writer's, so a
			// degraded commit stays as traceable as a healthy one.
			name:         "the campaign tag survives a degraded message",
			writerErr:    &autowrite.WriterError{Reason: "daemon not running"},
			prefix:       "[camp:abcd1234]",
			wantFallback: true,
			wantContains: []string{"[camp:abcd1234]", FallbackSubjectMarker},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := testCampaign(t)

			old := writeMessage
			writeMessage = func(context.Context, string, string, *Job) (string, error) {
				return tt.writerOut, tt.writerErr
			}
			t.Cleanup(func() { writeMessage = old })

			job := &Job{
				ID:            "job-fallback",
				Kind:          KindCommitTree,
				Repo:          ".",
				Tree:          "feedfacefeedfacefeedfacefeedfacefeedface",
				Parent:        "cafebabecafebabecafebabecafebabecafebabe",
				AutoWrite:     true,
				MessagePrefix: tt.prefix,
			}

			message, err := messageForTree(context.Background(), root, root, job)
			if err != nil {
				t.Fatalf("messageForTree() error = %v; a writer must never fail a commit job", err)
			}
			if strings.TrimSpace(message) == "" {
				t.Fatal("messageForTree() returned an empty message; the commit would have no subject")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(message, want) {
					t.Errorf("message %q does not contain %q", message, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(message, absent) {
					t.Errorf("message %q must not contain %q", message, absent)
				}
			}
			if tt.wantFallback && !strings.Contains(message, "camp wrote this message itself") {
				t.Errorf("message %q must say camp wrote it", message)
			}
		})
	}
}

// A degraded run is recorded where the only witness to a detached worker's
// decisions lives. Without the line, a commit carrying the marker has no
// account of why anywhere outside the commit itself.
func TestMessageForTreeLogsTheDegradation(t *testing.T) {
	root := testCampaign(t)

	old := writeMessage
	writeMessage = func(context.Context, string, string, *Job) (string, error) {
		return "", &autowrite.WriterError{Reason: "daemon not running"}
	}
	t.Cleanup(func() { writeMessage = old })

	job := &Job{
		ID: "job-degraded-log", Kind: KindCommitTree, Repo: ".",
		Tree: "feedface", Parent: "cafebabe", AutoWrite: true,
	}
	if _, err := messageForTree(context.Background(), root, root, job); err != nil {
		t.Fatalf("messageForTree() error = %v", err)
	}

	data, err := os.ReadFile(WorkerLogPath(root))
	if err != nil {
		t.Fatalf("read worker log: %v", err)
	}
	if log := string(data); !strings.Contains(log, "writer-degraded") || !strings.Contains(log, job.ID) {
		t.Fatalf("worker log does not record the degradation for %s:\n%s", job.ID, log)
	}
}

// A job that carries its own message and no writer is a different case: there
// is no unavailable tool to compensate for, only a malformed job document.
func TestMessageForTreeStillRejectsAMessagelessJob(t *testing.T) {
	root := testCampaign(t)
	job := &Job{ID: "job-no-message", Kind: KindCommitTree, Repo: "."}

	if _, err := messageForTree(context.Background(), root, root, job); err == nil {
		t.Fatal("a job with no message and no writer must fail rather than invent one")
	}
}

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
