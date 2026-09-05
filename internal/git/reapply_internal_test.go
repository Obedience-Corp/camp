package git

import (
	"strings"
	"testing"
)

// mergeTreeConflictOutput is what `git merge-tree --write-tree` prints when the
// re-application conflicts: the merged tree, one stage entry per side of every
// conflicted path, a blank line, then git's own report.
//
// Verbatim from git 2.54.0 rather than paraphrased, because the parsing below
// is the only thing standing between a user and a failure that names no files.
const mergeTreeConflictOutput = `386b1f59b8535305c3dbd607f3eae5e94e40a264
100644 5626abf0f72e58d7a153368ba57db4c673c0e171 1	a.txt
100644 b19a1e93bec1317dc6097229e12afaffbfa74dc2 2	a.txt
100644 950b81b7eee953d050aa05a641f8e056c85dd1bd 3	a.txt
100644 587be6b4c3f93f93c489c0111bba5596147a26cb 1	b.txt
100644 975fbec8256d3e8a3797e7a3611380f27c49f4ac 2	b.txt
100644 b68025345d5301abad4d9ec9166f455243a0d746 3	b.txt

Auto-merging a.txt
CONFLICT (content): Merge conflict in a.txt
Auto-merging b.txt
CONFLICT (content): Merge conflict in b.txt
`

func TestSplitMergeTreeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		out            string
		wantTree       string
		wantConflicted []string
		wantReportHas  string
	}{
		{
			name:           "a conflict names every path once",
			out:            mergeTreeConflictOutput,
			wantTree:       "386b1f59b8535305c3dbd607f3eae5e94e40a264",
			wantConflicted: []string{"a.txt", "b.txt"},
			wantReportHas:  "CONFLICT (content): Merge conflict in a.txt",
		},
		{
			name:     "a clean merge is the tree alone",
			out:      "d137fb7c0d25a7626a45039fa4bc42d310e23a9a\n",
			wantTree: "d137fb7c0d25a7626a45039fa4bc42d310e23a9a",
		},
		{
			// A path with a space is not a second field. Splitting on
			// whitespace rather than the tab would truncate it to "my".
			name: "a path containing spaces survives",
			out: "abc123\n100644 aaa 1\tmy notes/a b.md\n100644 bbb 2\tmy notes/a b.md\n\n" +
				"CONFLICT (content): Merge conflict in my notes/a b.md\n",
			wantTree:       "abc123",
			wantConflicted: []string{"my notes/a b.md"},
			wantReportHas:  "CONFLICT",
		},
		{
			name:     "empty output yields nothing rather than panicking",
			out:      "",
			wantTree: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree, conflicted, report := splitMergeTreeOutput(tt.out)
			if tree != tt.wantTree {
				t.Errorf("tree = %q, want %q", tree, tt.wantTree)
			}
			if len(conflicted) != len(tt.wantConflicted) {
				t.Fatalf("conflicted = %v, want %v", conflicted, tt.wantConflicted)
			}
			for i, want := range tt.wantConflicted {
				if conflicted[i] != want {
					t.Errorf("conflicted[%d] = %q, want %q", i, conflicted[i], want)
				}
			}
			if tt.wantReportHas != "" && !strings.Contains(report, tt.wantReportHas) {
				t.Errorf("report = %q, want it to contain %q", report, tt.wantReportHas)
			}
		})
	}
}

// The summary is what survives truncation, so what it says in its first bytes
// is the whole value of naming the paths at all.
func TestConflictSummary(t *testing.T) {
	t.Parallel()

	many := []string{"a", "b", "c", "d", "e", "f", "g", "h"}

	tests := []struct {
		name       string
		paths      []string
		want       string
		wantAbsent string
	}{
		{
			// git said something conflicted, so claiming zero files would
			// contradict the failure this is attached to.
			name:  "no parsable paths do not become a count of zero",
			paths: nil,
			want:  "(camp could not determine which paths)",
		},
		{
			name:  "one path is named on its own",
			paths: []string{".campaign/fest/navigation.yaml"},
			want:  "in .campaign/fest/navigation.yaml",
		},
		{
			name:  "a few paths are all named",
			paths: []string{"a.md", "b.md", "c.md"},
			want:  "in 3 files (a.md, b.md, c.md)",
		},
		{
			// The count stays exact even though the list is cut, so the user
			// knows how much they are not being shown.
			name:       "past the budget the rest are counted",
			paths:      many,
			want:       "in 8 files (a, b, c, d, e, f, and 2 more)",
			wantAbsent: "h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := conflictSummary(tt.paths)
			if got != tt.want {
				t.Fatalf("conflictSummary(%v) = %q, want %q", tt.paths, got, tt.want)
			}
			if tt.wantAbsent != "" && strings.Contains(got, tt.wantAbsent) {
				t.Errorf("summary %q must not list %q past the budget", got, tt.wantAbsent)
			}
		})
	}
}

// The conflicted paths have to lead the failure. A job's recorded error is
// truncated to a few hundred bytes, and git's own report opens with
// "Auto-merging" lines for the paths that merged fine — which is how the
// failure this fix was written for reported one conflicted file and then ran
// out of room before naming it.
func TestConflictedPathsLeadTheFailureText(t *testing.T) {
	t.Parallel()

	_, conflicted, report := splitMergeTreeOutput(mergeTreeConflictOutput)
	summary := conflictSummary(conflicted)
	failure := "re-applying the captured changes onto 8f0b8ac4 conflicted " + summary + ": " + report

	const recordedErrorBudget = 300
	if len(failure) > recordedErrorBudget {
		failure = failure[:recordedErrorBudget]
	}
	for _, path := range conflicted {
		if !strings.Contains(failure, path) {
			t.Errorf("truncated failure does not name %q:\n%s", path, failure)
		}
	}
}
