package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

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

func TestAppendGitignoreEntryIfMissingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	campaignDir := filepath.Join(dir, config.CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(campaignDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("state.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := appendGitignoreEntryIfMissing(dir, "workitems/current.yaml"); err != nil {
		t.Fatalf("appendGitignoreEntryIfMissing() error = %v", err)
	}
	first, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !gitignoreHasRule(string(first), "workitems/current.yaml") {
		t.Fatalf("gitignore missing appended rule:\n%s", first)
	}
	if !strings.Contains(string(first), "Per-machine current-workitem selection") {
		t.Fatalf("gitignore missing explanatory comment:\n%s", first)
	}

	if err := appendGitignoreEntryIfMissing(dir, "workitems/current.yaml"); err != nil {
		t.Fatalf("second appendGitignoreEntryIfMissing() error = %v", err)
	}
	second, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("second append should be idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
