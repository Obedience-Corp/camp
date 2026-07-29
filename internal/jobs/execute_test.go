package jobs

import "testing"

// The forbidden list is a claim about what the worker *can* do, not what it
// currently happens to do. A deferred job runs after the user's terminal has
// returned, against a working tree they are still editing: anything that
// stages by wildcard would capture whatever they changed since, and anything
// that moves history would rewrite commits they may already have built on.
func TestJobGitArgsNeverContainForbiddenOperations(t *testing.T) {
	jobs := []struct {
		name string
		job  Job
	}{
		{
			name: "bookkeeping commit",
			job: Job{Kind: KindCommitPaths, Repo: ".",
				Paths: []string{".campaign/intents/inbox/idea.md"}},
		},
		{
			name: "many paths",
			job: Job{Kind: KindCommitPaths, Repo: "projects/camp",
				Paths: []string{"a.md", "b.md", "c/d.md"}},
		},
		{
			name: "captured tree",
			job:  Job{Kind: KindCommitTree, Repo: ".", Tree: "9f2a", Message: "m"},
		},
		{
			// A path that merely looks like a flag must not become one: the
			// "--" separator is what keeps it an operand.
			name: "path resembling a flag",
			job: Job{Kind: KindCommitPaths, Repo: ".",
				Paths: []string{"--all", "-A", "."}},
		},
	}

	for _, tc := range jobs {
		t.Run(tc.name, func(t *testing.T) {
			args := GitArgsForJob(&tc.job)
			if op, bad := ViolatesForbiddenGit(args); bad {
				t.Errorf("job %q would run forbidden operation %q; args: %v", tc.name, op, args)
			}
		})
	}
}

// The detector must actually detect, or the test above proves nothing.
func TestViolatesForbiddenGitDetects(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "stage everything short", args: []string{"add", "-A"}, want: true},
		{name: "stage everything long", args: []string{"add", "--all"}, want: true},
		{name: "stage dot", args: []string{"add", "."}, want: true},
		{name: "commit all", args: []string{"commit", "-a", "-m", "x"}, want: true},
		{name: "amend", args: []string{"commit", "--amend"}, want: true},
		{name: "hard reset", args: []string{"reset", "--hard", "HEAD~1"}, want: true},
		{name: "rebase", args: []string{"rebase", "main"}, want: true},
		{name: "push", args: []string{"push", "origin", "main"}, want: true},
		{name: "checkout", args: []string{"checkout", "main"}, want: true},
		{name: "forbidden mid-list", args: []string{"-C", "/repo", "add", "-A"}, want: true},

		{name: "explicit add", args: []string{"add", "--", "a.md"}, want: false},
		{name: "explicit add of many", args: []string{"add", "--", "a.md", "b.md"}, want: false},
		{name: "commit with message", args: []string{"commit", "-m", "x"}, want: false},
		{name: "commit-tree", args: []string{"commit-tree", "9f2a"}, want: false},
		{
			// The separator means these are operands, not flags. A naive
			// substring check would call this forbidden and be wrong.
			name: "paths after -- that look like flags",
			args: []string{"add", "--", "--all", "-A", "."},
			want: false,
		},
		{name: "empty", args: nil, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := ViolatesForbiddenGit(tc.args)
			if got != tc.want {
				t.Errorf("ViolatesForbiddenGit(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestResolveRepoPathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, repo := range []string{"../outside", "../../elsewhere"} {
		t.Run(repo, func(t *testing.T) {
			if _, err := resolveRepoPath(root, repo); err == nil {
				t.Errorf("resolveRepoPath accepted %q", repo)
			}
		})
	}
}

func TestResolveRepoPathRootIsTheCampaign(t *testing.T) {
	root := t.TempDir()
	got, err := resolveRepoPath(root, ".")
	if err != nil {
		t.Fatalf("resolveRepoPath(.) error = %v", err)
	}
	if got != root {
		t.Errorf("resolveRepoPath(.) = %q, want %q", got, root)
	}
}

func TestIsLaneLockName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{name: "worker-%2E.lock", want: true},
		{name: "worker-projects%2Fcamp.lock", want: true},
		{name: "worker.log", want: false},
		{name: "worker-.lock", want: false},
		{name: "0000001.json", want: false},
		{name: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLaneLockName(tc.name); got != tc.want {
				t.Errorf("isLaneLockName(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
