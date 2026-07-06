package worktrees

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	intworktree "github.com/Obedience-Corp/camp/internal/worktree"
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

func TestFilterRegisteredWorktreeNames_DropsCopiedCheckoutChildren(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "campaign")
	pm := intworktree.NewPathManager(paths.NewResolver(root, config.DefaultCampaignPaths()))

	got := filterRegisteredWorktreeNames("fest-gif-review",
		[]string{"bin", "node_modules", "scripts", "src", "real-review"},
		[]intworktree.GitWorktreeEntry{{
			Path: pm.WorktreePath("fest-gif-review", "real-review"),
		}},
		pm,
	)
	want := []string{"real-review"}

	if !slices.Equal(got, want) {
		t.Fatalf("filterRegisteredWorktreeNames() = %v, want %v", got, want)
	}
}
