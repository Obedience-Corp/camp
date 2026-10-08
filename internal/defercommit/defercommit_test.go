package defercommit

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jobs"
)

// Each refusal below is a correctness rule, not a preference. A copy of this
// decision that drifted by one condition would silently either skip a user's
// git hook or break the synchronous contract --json consumers depend on, so
// every reason gets its own test naming what it protects.

func TestAllowedRefusals(t *testing.T) {
	// A path inside a campaign that resolves to a lane. Hook detection is the
	// only condition that shells out to git, and it is checked last, so these
	// cases never reach it.
	const campaignRoot = "/campaigns/demo"
	const repoPath = "/campaigns/demo/projects/camp"

	tests := []struct {
		name string
		env  string
		req  Request
		want Refusal
	}{
		{
			name: "CAMP_NO_DEFER turns deferral off entirely",
			env:  "1",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath},
			want: RefusedDisabled,
		},
		{
			name: "a non-zero value other than 1 still means off",
			env:  "false",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath},
			want: RefusedDisabled,
		},
		{
			name: "--json is synchronous by contract",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath, JSON: true},
			want: RefusedJSON,
		},
		{
			name: "--amend rewrites HEAD so the parent check cannot apply",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: repoPath, Amend: true},
			want: RefusedAmend,
		},
		{
			name: "outside a campaign there is no queue",
			req:  Request{CampaignRoot: "", RepoPath: repoPath},
			want: RefusedNoCampaign,
		},
		{
			name: "a repo outside the campaign tree has no lane",
			req:  Request{CampaignRoot: campaignRoot, RepoPath: "/somewhere/else"},
			want: RefusedNoCampaign,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvNoDefer, tt.env)
			allowed, why := Allowed(context.Background(), tt.req)
			if allowed {
				t.Fatalf("deferral was allowed; want refusal %q", tt.want)
			}
			if why != tt.want {
				t.Errorf("refusal = %q, want %q", why, tt.want)
			}
		})
	}
}

// Camp's own bookkeeping defers under narrower rules than an --auto-write
// commit: it needs no writer, but it needs explicit paths and must never
// depend on the user's index.
func TestAllowedForPaths(t *testing.T) {
	const campaignRoot = "/campaigns/demo"
	const repoPath = "/campaigns/demo/projects/camp"
	paths := []string{".campaign/intents/inbox/idea.md"}

	tests := []struct {
		name      string
		env       string
		paths     []string
		preStaged []string
		root      string
		want      Refusal
	}{
		{
			name: "CAMP_NO_DEFER turns it off here too",
			env:  "1", paths: paths, root: campaignRoot,
			want: RefusedDisabled,
		},
		{
			// A deferred job runs at an unknown later moment. Without explicit
			// paths the synchronous path would stage everything present then,
			// which is work the user never associated with this commit.
			name:  "no explicit paths",
			paths: nil, root: campaignRoot,
			want: RefusedNoPaths,
		},
		{
			// Pre-staged content is read out of the user's real index, now. A
			// job running later would commit the worktree instead, which is a
			// different commit than the one asked for.
			name:  "the commit depends on already-staged content",
			paths: paths, preStaged: []string{"other.md"}, root: campaignRoot,
			want: RefusedPreStaged,
		},
		{
			name:  "outside a campaign there is no queue",
			paths: paths, root: "",
			want: RefusedNoCampaign,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvNoDefer, tt.env)
			allowed, why := AllowedForPaths(
				context.Background(), tt.root, repoPath, tt.paths, tt.preStaged)
			if allowed {
				t.Fatalf("deferral was allowed; want refusal %q", tt.want)
			}
			if why != tt.want {
				t.Errorf("refusal = %q, want %q", why, tt.want)
			}
		})
	}
}

// "0" and empty are the only values that leave deferral on. Someone exporting
// CAMP_NO_DEFER=false is asking for it off, and guessing the other way would
// defer for a user who explicitly said not to.
func TestDisabled(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"1", true},
		{"true", true},
		{"false", true},
		{" 1 ", true},
	}
	for _, tt := range tests {
		t.Run("value="+tt.value, func(t *testing.T) {
			t.Setenv(EnvNoDefer, tt.value)
			if got := Disabled(); got != tt.want {
				t.Errorf("Disabled() with %q = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// Refusals are ordered so a user hears about their own configuration before
// camp's internal constraints, and so the only condition that shells out to
// git runs last.
func TestRefusalOrderPutsTheUsersOwnSwitchFirst(t *testing.T) {
	t.Setenv(EnvNoDefer, "1")
	_, why := Allowed(context.Background(), Request{
		CampaignRoot: "/campaigns/demo",
		RepoPath:     "/campaigns/demo/projects/camp",
		JSON:         true,
		Amend:        true,
	})
	if why != RefusedDisabled {
		t.Errorf("refusal = %q; the user's own switch must be reported first", why)
	}
}

// queuedJobFiles counts the job documents a campaign's queue holds.
func queuedJobFiles(t *testing.T, campaignRoot string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(jobs.QueueDir(campaignRoot), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // the queue directory need not exist
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".json" {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk queue: %v", err)
	}
	return count
}

// A capture camp cannot complete refuses the job. It must never fall back to a
// path-only one.
//
// A path-only job is not a coarser promise, it is a different one: it commits
// whatever those paths hold when the worker runs, minutes later, under a
// message written for what they held at enqueue. Nothing in the resulting
// commit says the substitution happened, which is why it cannot be the
// fallback — the caller's fallback is a synchronous commit of exactly the
// content the user is looking at.
func TestEnqueuePathsRefusesAnIncompleteCapture(t *testing.T) {
	original := captureBlobs
	t.Cleanup(func() { captureBlobs = original })

	cases := []struct {
		name       string
		captureErr error
	}{
		{
			name:       "nested repository",
			captureErr: camperrors.Wrapf(git.ErrNestedRepo, "projects/festival-wails"),
		},
		{
			// The live failure: a symlink git could not hash by name. Anything
			// camp cannot read is this case.
			name: "path camp could not read",
			captureErr: camperrors.New(
				"capture projects/obey-voice: git hash-object -w -- projects/obey-voice: fatal: Unable to add"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			captureBlobs = func(context.Context, string, []string) ([]git.BlobRef, error) {
				return nil, tc.captureErr
			}

			job, err := EnqueuePaths(context.Background(), root, root, "bookkeeping", []string{"note.md"})
			if !errors.Is(err, tc.captureErr) {
				t.Fatalf("EnqueuePaths() error = %v, want the capture failure", err)
			}
			if job != nil {
				t.Fatalf("EnqueuePaths() job = %+v, want none; a degraded job commits execution-time content", job)
			}
			if n := queuedJobFiles(t, root); n != 0 {
				t.Fatalf("queue holds %d job(s) after a refused capture, want 0", n)
			}
		})
	}
}

// The other half of the contract: a capture that succeeds queues a job that
// carries the captured content, so the commit is a function of enqueue time.
func TestEnqueuePathsQueuesCapturedContent(t *testing.T) {
	original := captureBlobs
	t.Cleanup(func() { captureBlobs = original })

	root := t.TempDir()
	captureBlobs = func(context.Context, string, []string) ([]git.BlobRef, error) {
		return []git.BlobRef{{Path: "note.md", Mode: "100644", SHA: "0123456789abcdef0123456789abcdef01234567"}}, nil
	}

	job, err := EnqueuePaths(context.Background(), root, root, "bookkeeping", []string{"note.md"})
	if err != nil {
		t.Fatalf("EnqueuePaths() error = %v", err)
	}
	if len(job.Blobs) != 1 || job.Blobs[0].Path != "note.md" {
		t.Fatalf("job blobs = %+v, want the captured note.md", job.Blobs)
	}
	if n := queuedJobFiles(t, root); n != 1 {
		t.Fatalf("queue holds %d job(s), want 1", n)
	}
}

// Cancellation is checked before anything is captured or written.
func TestEnqueuePathsHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnqueuePaths(ctx, t.TempDir(), t.TempDir(), "m", []string{"note.md"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnqueuePaths() error = %v, want context.Canceled", err)
	}
}
