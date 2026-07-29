package worktree

import "testing"

func TestIsLinkedWorktree(t *testing.T) {
	const projectPath = "/campaign/projects/camp"

	tests := []struct {
		name  string
		entry GitWorktreeEntry
		want  bool
	}{
		{
			name:  "bare entry skipped",
			entry: GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/x", IsBare: true},
			want:  false,
		},
		{
			name:  "empty path skipped",
			entry: GitWorktreeEntry{Path: ""},
			want:  false,
		},
		{
			name:  "main working tree skipped",
			entry: GitWorktreeEntry{Path: projectPath, Branch: "main"},
			want:  false,
		},
		{
			name:  "submodule main worktree under .git skipped",
			entry: GitWorktreeEntry{Path: "/campaign/.git/modules/projects/camp", Branch: "main"},
			want:  false,
		},
		{
			name:  "hidden worktree directory skipped",
			entry: GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/.tmp"},
			want:  false,
		},
		{
			name:  "preferred-location worktree",
			entry: GitWorktreeEntry{Path: "/campaign/projects/worktrees/camp/feature-auth", Branch: "feature-auth"},
			want:  true,
		},
		{
			name:  "non-preferred-location worktree still resolves",
			entry: GitWorktreeEntry{Path: "/campaign/projects/worktrees/fix-camp-392", IsDetached: true},
			want:  true,
		},
		{
			name:  "worktree entirely outside the worktrees dir still resolves",
			entry: GitWorktreeEntry{Path: "/tmp/scratch/camp-experiment", Branch: "experiment"},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLinkedWorktree(projectPath, tc.entry); got != tc.want {
				t.Errorf("IsLinkedWorktree() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestContainsGitDir(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/campaign/.git/modules/projects/camp", true},
		{"/campaign/projects/worktrees/camp/feature", false},
		{"/campaign/projects/worktrees/fix-camp-392", false},
		{".git/worktrees/x", true},
		{"/campaign/projects/camp", false},
	}

	for _, tc := range tests {
		if got := containsGitDir(tc.path); got != tc.want {
			t.Errorf("containsGitDir(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
