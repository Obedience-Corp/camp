package worktrees

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/project"
)

func captureWorktreesStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		return "", readErr
	}
	return string(out), runErr
}

func TestOutputListTable_RendersColumns(t *testing.T) {
	result := &WorktreeListResult{
		Worktrees: []WorktreeListItem{
			{Project: "camp", Name: "ic-feature-x", Branch: "ic-feature-x", LastAccessed: "2 days ago", Stale: false},
			{Project: "fest", Name: "ic-fix-y", Branch: "ic-fix-y", LastAccessed: "1 hour ago", Stale: true, StaleReason: "missing .git"},
		},
		Total:      2,
		StaleCount: 1,
	}

	out, err := captureWorktreesStdout(func() error {
		return outputListTable(result)
	})
	if err != nil {
		t.Fatalf("outputListTable: %v", err)
	}

	for _, col := range []string{"PROJECT", "NAME", "BRANCH", "LAST ACCESSED", "STATUS"} {
		if !strings.Contains(out, col) {
			t.Errorf("outputListTable output missing column header %q", col)
		}
	}
	if !strings.Contains(out, "ic-feature-x") {
		t.Error("outputListTable missing worktree name ic-feature-x")
	}
	if !strings.Contains(out, "camp") {
		t.Error("outputListTable missing project name camp")
	}
	if !strings.Contains(out, "2 worktree(s)") {
		t.Errorf("outputListTable missing count line, got:\n%s", out)
	}
	if !strings.Contains(out, "(1 stale)") {
		t.Errorf("outputListTable missing stale count, got:\n%s", out)
	}
}

func TestDisambiguateWorktreeNames_SameBasenameDifferentPath(t *testing.T) {
	campRoot := "/camp"
	worktrees := []WorktreeListItem{
		// Two linked worktrees for the same project sharing a basename "foo":
		// one preferred (inside the campaign), one loose (outside).
		{Project: "proj", Name: "foo", Path: "/camp/projects/worktrees/proj/foo"},
		{Project: "proj", Name: "foo", Path: "/elsewhere/foo"},
		// A distinct basename in the same project must be left untouched.
		{Project: "proj", Name: "bar", Path: "/camp/projects/worktrees/proj/bar"},
		// A same basename under a DIFFERENT project does not collide.
		{Project: "other", Name: "foo", Path: "/camp/projects/worktrees/other/foo"},
	}

	disambiguateWorktreeNames(campRoot, worktrees)

	// The colliding "proj/foo" pair is rewritten to unique, path-derived names.
	if worktrees[0].Name != filepath.FromSlash("projects/worktrees/proj/foo") {
		t.Errorf("preferred colliding worktree name = %q, want campaign-relative path", worktrees[0].Name)
	}
	if worktrees[1].Name != filepath.FromSlash("/elsewhere/foo") {
		t.Errorf("loose colliding worktree name = %q, want absolute path", worktrees[1].Name)
	}
	if worktrees[0].Name == worktrees[1].Name {
		t.Errorf("colliding worktrees must get distinct names, both = %q", worktrees[0].Name)
	}
	// Non-colliding entries keep their basename.
	if worktrees[2].Name != "bar" {
		t.Errorf("non-colliding name changed: %q, want bar", worktrees[2].Name)
	}
	if worktrees[3].Name != "foo" {
		t.Errorf("same basename in another project must not be disambiguated: %q, want foo", worktrees[3].Name)
	}
}

func TestOutputListTable_Empty(t *testing.T) {
	result := &WorktreeListResult{Worktrees: nil, Total: 0, StaleCount: 0}

	out, err := captureWorktreesStdout(func() error {
		return outputListTable(result)
	})
	if err != nil {
		t.Fatalf("outputListTable: %v", err)
	}
	if !strings.Contains(out, "No worktrees found") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestOutputListTable_StaleReasonInStatus(t *testing.T) {
	result := &WorktreeListResult{
		Worktrees: []WorktreeListItem{
			{Project: "camp", Name: "wt-broken", Branch: "unknown", LastAccessed: "5 days ago", Stale: true, StaleReason: "missing .git"},
		},
		Total:      1,
		StaleCount: 1,
	}

	out, err := captureWorktreesStdout(func() error {
		return outputListTable(result)
	})
	if err != nil {
		t.Fatalf("outputListTable: %v", err)
	}
	if !strings.Contains(out, "missing .git") {
		t.Errorf("outputListTable missing stale reason in output, got:\n%s", out)
	}
}

func TestPathWithin(t *testing.T) {
	cases := []struct {
		name   string
		child  string
		parent string
		want   bool
	}{
		{name: "equal", child: "/camp/projects/camp", parent: "/camp/projects/camp", want: true},
		{name: "nested child", child: "/camp/projects/camp/cmd", parent: "/camp/projects/camp", want: true},
		{name: "sibling", child: "/camp/projects/fest", parent: "/camp/projects/camp", want: false},
		{name: "parent is child of child", child: "/camp", parent: "/camp/projects/camp", want: false},
		{name: "empty child", child: "", parent: "/camp", want: false},
		{name: "empty parent", child: "/camp", parent: "", want: false},
		{name: "both empty", child: "", parent: "", want: false},
		{name: "prefix trick not under", child: "/camp-other/x", parent: "/camp", want: false},
		{name: "trailing slash cleaned", child: "/camp/projects/camp/", parent: "/camp/projects/camp", want: true},
		{name: "dot segment cleaned", child: "/camp/projects/camp/./cmd", parent: "/camp/projects/camp", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithin(tc.child, tc.parent); got != tc.want {
				t.Errorf("pathWithin(%q, %q) = %v, want %v", tc.child, tc.parent, got, tc.want)
			}
		})
	}
}

func TestMatchRegisteredProject(t *testing.T) {
	campRoot := "/campaign"
	projects := []project.Project{
		{Name: "camp", Path: "projects/camp"},
		{Name: "nested", Path: "projects/camp/packages/nested"},
		{Name: "fest", Path: "projects/fest"},
	}

	t.Run("main checkout", func(t *testing.T) {
		got := matchRegisteredProject("/campaign/projects/camp", campRoot, projects)
		if got == nil || got.name != "camp" {
			t.Fatalf("got %#v, want camp", got)
		}
	})
	t.Run("nested monorepo wins longest path", func(t *testing.T) {
		got := matchRegisteredProject("/campaign/projects/camp/packages/nested/pkg", campRoot, projects)
		if got == nil || got.name != "nested" {
			t.Fatalf("got %#v, want nested", got)
		}
	})
	t.Run("campaign root no match", func(t *testing.T) {
		if got := matchRegisteredProject("/campaign", campRoot, projects); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})
	t.Run("worktree path no registered match", func(t *testing.T) {
		// Linked worktrees sit beside the checkout; registered match must miss.
		got := matchRegisteredProject("/campaign/projects/worktrees/camp/feature", campRoot, projects)
		if got != nil {
			t.Fatalf("expected nil for worktree path, got %#v", got)
		}
	})
	t.Run("empty cwd", func(t *testing.T) {
		if got := matchRegisteredProject("", campRoot, projects); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})
}

func TestTargetsFromProjects(t *testing.T) {
	campRoot := "/campaign"
	projects := []project.Project{
		{Name: "camp", Path: "projects/camp"},
		{Name: "fest", Path: "projects/fest"},
	}
	got := targetsFromProjects(campRoot, projects)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].name != "camp" || got[0].path != filepath.Join(campRoot, "projects/camp") {
		t.Errorf("first = %#v", got[0])
	}
	if got[1].name != "fest" || got[1].path != filepath.Join(campRoot, "projects/fest") {
		t.Errorf("second = %#v", got[1])
	}
}
