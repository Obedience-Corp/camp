package jobs

import "testing"

// The explicit-paths rule is the queue's central safety property. A deferred
// job runs at an unknown later moment, so a glob or a "." would stage whatever
// happens to be present then, sweeping in work the user never associated with
// this commit. Error cases first.
func TestJobValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		job  Job
	}{
		{name: "unknown kind", job: Job{Kind: "commit", Repo: ".", Paths: []string{"a"}}},
		{name: "empty kind", job: Job{Repo: ".", Paths: []string{"a"}}},
		{name: "empty repo", job: Job{Kind: KindCommitPaths, Paths: []string{"a"}}},
		{name: "absolute repo", job: Job{Kind: KindCommitPaths, Repo: "/tmp/x", Paths: []string{"a"}}},
		{name: "escaping repo", job: Job{Kind: KindCommitPaths, Repo: "../x", Paths: []string{"a"}}},

		{name: "commit-paths with no paths", job: Job{Kind: KindCommitPaths, Repo: "."}},
		{name: "commit-paths with empty path", job: Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{""}}},
		{
			name: "commit-paths with dot",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"."}},
		},
		{
			name: "commit-paths with star",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"*"}},
		},
		{
			name: "commit-paths with a glob",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"docs/*.md"}},
		},
		{
			name: "commit-paths with a character class",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"docs/[ab].md"}},
		},
		{
			name: "commit-paths with an absolute path",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"/etc/passwd"}},
		},
		{
			name: "commit-paths escaping the repo",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"../outside.md"}},
		},

		{name: "commit-tree with no tree", job: Job{Kind: KindCommitTree, Repo: ".", Message: "m"}},
		{
			name: "commit-tree with neither message nor auto_write",
			job:  Job{Kind: KindCommitTree, Repo: ".", Tree: "9f2a"},
		},

		{
			// Criterion 37m's data-shape half: a follow-up may not carry its
			// own follow-up. One level keeps the worker's chaining bounded.
			name: "then of then",
			job: Job{
				Kind: KindCommitPaths, Repo: ".", Paths: []string{"a"},
				Then: &Follow{Kind: KindCommitTree, Repo: ".", Paths: []string{"b"}},
			},
		},
		{
			name: "then with no paths",
			job: Job{
				Kind: KindCommitPaths, Repo: ".", Paths: []string{"a"},
				Then: &Follow{Kind: KindCommitPaths, Repo: "."},
			},
		},
		{
			name: "then with an escaping repo",
			job: Job{
				Kind: KindCommitPaths, Repo: ".", Paths: []string{"a"},
				Then: &Follow{Kind: KindCommitPaths, Repo: "../x", Paths: []string{"b"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.job.Validate(); err == nil {
				t.Error("Validate() = nil, want a rejection")
			}
		})
	}
}

func TestJobValidateAccepts(t *testing.T) {
	cases := []struct {
		name string
		job  Job
	}{
		{
			name: "campaign-root bookkeeping",
			job: Job{Kind: KindCommitPaths, Repo: ".",
				Paths: []string{".campaign/intents/inbox/idea.md"}},
		},
		{
			name: "submodule bookkeeping",
			job:  Job{Kind: KindCommitPaths, Repo: "projects/camp", Paths: []string{"docs/a.md"}},
		},
		{
			name: "captured tree with a message",
			job:  Job{Kind: KindCommitTree, Repo: ".", Tree: "9f2a", Parent: "6000fd", Message: "m"},
		},
		{
			name: "captured tree with auto_write and no message",
			job:  Job{Kind: KindCommitTree, Repo: ".", Tree: "9f2a", AutoWrite: true},
		},
		{
			name: "one level of follow-up",
			job: Job{Kind: KindCommitPaths, Repo: "projects/camp", Paths: []string{"a.md"},
				Then: &Follow{Kind: KindCommitPaths, Repo: ".", Paths: []string{"projects/camp"}}},
		},
		{
			// A path containing a hyphen or dot is ordinary, not a glob.
			name: "ordinary punctuation is not a glob",
			job: Job{Kind: KindCommitPaths, Repo: ".",
				Paths: []string{"a-file.name.md", "dir/sub.dir/f.txt"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.job.Validate(); err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestKindValid(t *testing.T) {
	cases := []struct {
		kind Kind
		want bool
	}{
		{kind: KindCommitPaths, want: true},
		{kind: KindCommitTree, want: true},
		{kind: "", want: false},
		// The design doc's early sample used "commit"; it is not a kind.
		{kind: "commit", want: false},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := tc.kind.Valid(); got != tc.want {
				t.Errorf("Kind(%q).Valid() = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestPadSeqAndSeqFromFilename(t *testing.T) {
	cases := []struct {
		seq  int
		want string
	}{
		{seq: 1, want: "0000001"},
		{seq: 42, want: "0000042"},
		{seq: 1234567, want: "1234567"},
	}
	for _, tc := range cases {
		if got := padSeq(tc.seq); got != tc.want {
			t.Errorf("padSeq(%d) = %q, want %q", tc.seq, got, tc.want)
		}
		name := jobFilename(tc.seq)
		got, ok := seqFromFilename(name)
		if !ok || got != tc.seq {
			t.Errorf("seqFromFilename(%q) = (%d, %v), want (%d, true)", name, got, ok, tc.seq)
		}
	}
}

// Zero padding exists so a lexical sort is execution order. Without it, job 10
// would sort before job 9 and the queue would run out of order.
func TestLexicalOrderIsExecutionOrder(t *testing.T) {
	names := []string{
		jobFilename(9),
		jobFilename(10),
		jobFilename(2),
		jobFilename(100),
	}
	want := []int{2, 9, 10, 100}

	sorted := append([]string(nil), names...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for i, name := range sorted {
		got, _ := seqFromFilename(name)
		if got != want[i] {
			t.Errorf("lexical position %d = seq %d, want %d (order: %v)", i, got, want[i], sorted)
		}
	}
}

func TestSeqFromFilenameRejectsMalformed(t *testing.T) {
	for _, name := range []string{"", "notanumber.json", "0000001", "abc.json", ".json"} {
		if _, ok := seqFromFilename(name); ok {
			t.Errorf("seqFromFilename(%q) accepted a malformed name", name)
		}
	}
}
