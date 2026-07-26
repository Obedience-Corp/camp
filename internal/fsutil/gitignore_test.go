package fsutil_test

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/fsutil"
)

func TestHasGitignoreRule(t *testing.T) {
	const content = `# Machine-local campaign state
.campaign/cache/

media/renders/
# videos/
not-videos/
  spaced/
`

	cases := []struct {
		name  string
		entry string
		want  bool
	}{
		{name: "empty entry is never present", entry: "", want: false},
		{name: "whitespace entry is never present", entry: "   ", want: false},
		{name: "commented-out rule does not count", entry: "videos/", want: false},
		{name: "substring of another rule does not count", entry: "os/", want: false},
		{name: "exact active rule", entry: "media/renders/", want: true},
		{name: "rule matched after trimming the line", entry: "spaced/", want: true},
		{name: "entry matched after trimming the entry", entry: "  media/renders/  ", want: true},
		{name: "rule that only appears as a prefix", entry: "media/", want: false},
		{name: "absent rule", entry: "datasets/", want: false},
		{name: "the literal prefixed rule is present", entry: "not-videos/", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsutil.HasGitignoreRule(content, tc.entry); got != tc.want {
				t.Errorf("HasGitignoreRule(_, %q) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}

// A commented-out rule must not read as present: git would still track the
// file, so reporting it as ignored would be wrong in the direction that loses
// data.
func TestHasGitignoreRuleIgnoresCommentsAndBlanks(t *testing.T) {
	if fsutil.HasGitignoreRule("# media/renders/\n\n", "media/renders/") {
		t.Error("HasGitignoreRule() = true for a commented-out rule, want false")
	}
	if fsutil.HasGitignoreRule("", "media/renders/") {
		t.Error("HasGitignoreRule() = true for empty content, want false")
	}
}
