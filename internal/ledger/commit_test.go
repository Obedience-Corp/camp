package ledger

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoLabel(t *testing.T) {
	root := "/campaign"
	assert.Equal(t, "campaign-root", RepoLabel(root, root))
	assert.Equal(t, "campaign-root", RepoLabel(root, ""))
	assert.Equal(t, "projects/camp", RepoLabel(root, filepath.Join(root, "projects", "camp")))
	assert.Equal(t, "projects/nested/tool", RepoLabel(root, filepath.Join(root, "projects", "nested", "tool")))
}

// TestRepoLabelMatchesDoctorResolution documents the producer/doctor contract:
// a label from RepoLabel must join back under the campaign root without an
// extra "projects/" prefix (the bug was projects/projects/camp).
func TestRepoLabelMatchesDoctorResolution(t *testing.T) {
	root := "/Users/me/obey-campaign"
	cases := []struct {
		repoPath string
		wantRel  string // path relative to root after doctor resolve
	}{
		{root, ""}, // campaign-root → root itself
		{filepath.Join(root, "projects", "camp"), "projects/camp"},
	}
	for _, tc := range cases {
		label := RepoLabel(root, tc.repoPath)
		var resolved string
		switch label {
		case "", "campaign-root", ".":
			resolved = root
		default:
			// doctor: slash labels join to campaign root; bare names under projects/
			if filepath.Base(label) != label {
				resolved = filepath.Join(root, filepath.FromSlash(label))
			} else {
				resolved = filepath.Join(root, "projects", label)
			}
		}
		assert.Equal(t, tc.repoPath, resolved, "label %q must round-trip", label)
		if tc.wantRel != "" {
			assert.Equal(t, tc.wantRel, label)
		}
	}
}
