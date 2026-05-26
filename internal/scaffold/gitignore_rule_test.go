package scaffold

import "testing"

func TestGitignoreHasRule(t *testing.T) {
	cases := []struct {
		name    string
		content string
		entry   string
		want    bool
	}{
		{
			name:    "empty file",
			content: "",
			entry:   "workitems/current.yaml",
			want:    false,
		},
		{
			name:    "exact match on its own line",
			content: "state.yaml\nworkitems/current.yaml\n",
			entry:   "workitems/current.yaml",
			want:    true,
		},
		{
			name:    "exact match with surrounding whitespace",
			content: "state.yaml\n  workitems/current.yaml   \n",
			entry:   "workitems/current.yaml",
			want:    true,
		},
		{
			name:    "commented out line is not a rule",
			content: "# workitems/current.yaml\nstate.yaml\n",
			entry:   "workitems/current.yaml",
			want:    false,
		},
		{
			name:    "commented out with leading whitespace is not a rule",
			content: "    # workitems/current.yaml\n",
			entry:   "workitems/current.yaml",
			want:    false,
		},
		{
			name:    "substring suffix is not a match",
			content: "not-workitems/current.yaml\n",
			entry:   "workitems/current.yaml",
			want:    false,
		},
		{
			name:    "substring prefix is not a match",
			content: "workitems/current.yaml.broken\n",
			entry:   "workitems/current.yaml",
			want:    false,
		},
		{
			name:    "blank lines are skipped",
			content: "\n\nworkitems/current.yaml\n\n",
			entry:   "workitems/current.yaml",
			want:    true,
		},
		{
			name:    "rule alongside comment",
			content: "# Per-machine current-workitem selection\nworkitems/current.yaml\n",
			entry:   "workitems/current.yaml",
			want:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitignoreHasRule(tc.content, tc.entry); got != tc.want {
				t.Errorf("gitignoreHasRule(%q, %q) = %v, want %v",
					tc.content, tc.entry, got, tc.want)
			}
		})
	}
}
