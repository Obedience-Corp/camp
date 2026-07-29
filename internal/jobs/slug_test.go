package jobs

import "testing"

// Lanes must be injective. Two repos sharing a lane would serialize against
// each other for no reason, and their sequence numbers would interleave so no
// listing could say which job belongs to which repo.
func TestLaneSlugIsCollisionFree(t *testing.T) {
	repos := []string{
		".",
		"",
		"./",
		"root", // must not collide with the campaign root
		"projects/camp",
		"projects/fest",
		"projects/worktrees/camp/feature", // nested worktree path
		"projects/worktrees/camp/other",
		"weird%2Fname", // a literal that could decode ambiguously
		"weird/name",
		"a%25b",
		"a%b",
	}

	// "." "" and "./" are three spellings of the campaign root and must share
	// a lane; everything else must be distinct.
	rootSpellings := map[string]bool{".": true, "": true, "./": true}

	seen := make(map[string]string)
	for _, repo := range repos {
		slug := LaneSlug(repo)
		if rootSpellings[repo] {
			if slug != rootLaneSlug {
				t.Errorf("LaneSlug(%q) = %q, want the root lane %q", repo, slug, rootLaneSlug)
			}
			continue
		}
		if prev, dup := seen[slug]; dup {
			t.Errorf("LaneSlug(%q) collides with LaneSlug(%q): both %q", repo, prev, slug)
		}
		seen[slug] = repo
		if slug == rootLaneSlug {
			t.Errorf("LaneSlug(%q) = %q, which is the campaign root's lane", repo, slug)
		}
	}
}

func TestLaneSlugRoundTrips(t *testing.T) {
	cases := []string{
		".",
		"projects/camp",
		"projects/worktrees/camp/feature",
		"weird/name",
		"a%b",
		"root",
	}
	for _, repo := range cases {
		t.Run(repo, func(t *testing.T) {
			if got := RepoFromLaneSlug(LaneSlug(repo)); got != normalizeRepo(repo) {
				t.Errorf("round trip of %q = %q, want %q", repo, got, normalizeRepo(repo))
			}
		})
	}
}

// A lane slug is one directory name, so it must never contain a separator that
// would nest it or let it walk out of the queue.
//
// A leading ".." in the *encoded* form is not a traversal: the separator is
// percent-encoded, so "..%2Fescape" is a literal directory name. What must not
// happen is a real separator surviving the encoding.
func TestLaneSlugIsASingleSegment(t *testing.T) {
	for _, repo := range []string{".", "projects/camp", "a/b/c/d", "weird/../name"} {
		slug := LaneSlug(repo)
		for _, bad := range []string{"/", "\\"} {
			if containsSegment(slug, bad) {
				t.Errorf("LaneSlug(%q) = %q contains separator %q", repo, slug, bad)
			}
		}
	}
}

// Escaping repo paths never reach a lane at all: they are rejected when the
// job is enqueued, because a job whose repo is outside the campaign would have
// the worker commit into a repository the campaign does not own.
func TestEscapingRepoIsRejectedBeforeItBecomesALane(t *testing.T) {
	cases := []string{"../escape", "../../elsewhere", "/absolute/repo"}
	for _, repo := range cases {
		t.Run(repo, func(t *testing.T) {
			job := Job{Kind: KindCommitPaths, Repo: repo, Paths: []string{"a.txt"}}
			if err := job.Validate(); err == nil {
				t.Errorf("Validate() accepted repo %q, want rejection", repo)
			}
		})
	}
}

func containsSegment(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
