package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineLocal(t *testing.T) {
	root := t.TempDir()
	gitmodules := "[submodule \"camp\"]\n\tpath = projects/camp\n\turl = git@github.com:Obedience-Corp/camp.git\n"
	if err := os.WriteFile(filepath.Join(root, ".gitmodules"), []byte(gitmodules), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		scope LinkScope
		want  bool
		why   string
	}{
		{
			name:  "worktree",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/fest/x"},
			want:  true,
			why:   "camp gitignores worktrees as machine-local",
		},
		{
			name:  "declared submodule",
			scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"},
			want:  true,
			why:   "an uncloned submodule is absent, not deleted",
		},
		{
			name:  "undeclared project",
			scope: LinkScope{Kind: ScopeProject, Path: "projects/not-a-submodule"},
			want:  false,
		},
		{
			name:  "campaign path",
			scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/x"},
			want:  false,
			why:   "workflow/ is tracked, so absence is authoritative",
		},
		{
			name:  "festival",
			scope: LinkScope{Kind: ScopeFestival, Path: "festivals/active/x"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MachineLocal(root, tc.scope); got != tc.want {
				t.Fatalf("MachineLocal(%v) = %v, want %v (%s)", tc.scope, got, tc.want, tc.why)
			}
		})
	}
}

// links.yaml is tracked, so a row removed here is removed on every machine the
// campaign syncs to. Pruning a worktree row because the worktree is not on this
// machine would destroy a link that is correct on another one.
func TestPruneDead_NeverRemovesMachineLocalRows(t *testing.T) {
	root := t.TempDir()

	worktree := Link{
		ID: "lnk_20260726_aaaaaa", WorkitemID: "design-a-2026-07-26",
		Scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/fest/elsewhere"},
		Role:  RolePrimary,
	}
	deadDesign := Link{
		ID: "lnk_20260726_bbbbbb", WorkitemID: "design-b-2026-07-26",
		Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/deleted"},
		Role:  RolePrimary,
	}
	live := Link{
		ID: "lnk_20260726_cccccc", WorkitemID: "design-c-2026-07-26",
		Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/here"},
		Role:  RolePrimary,
	}
	if err := os.MkdirAll(filepath.Join(root, "workflow", "design", "here"), 0o755); err != nil {
		t.Fatal(err)
	}

	l := &Links{Version: LinksSchemaVersion, Links: []Link{worktree, deadDesign, live}}
	removed := PruneDead(root, l)

	if len(removed) != 1 || removed[0].Link.ID != deadDesign.ID {
		t.Fatalf("removed = %+v, want only the dead campaign path", removed)
	}
	if len(l.Links) != 2 {
		t.Fatalf("registry has %d rows, want 2", len(l.Links))
	}
	for _, want := range []string{worktree.ID, live.ID} {
		if _, ok := l.FindByID(want); !ok {
			t.Fatalf("row %s was pruned and must not have been", want)
		}
	}
}

// A missing workitem is deliberately NOT an auto-prune signal: answering it
// needs a full tree walk, and pruning on a scan means any gap in it, or a
// branch that does not carry the workitem, silently deletes live links.
// camp workitem doctor owns that case.
func TestPruneDead_IgnoresMissingWorkitem(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflow", "design", "here"), 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := Link{
		ID: "lnk_20260726_dddddd", WorkitemID: "design-gone-2026-07-26",
		Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/here"},
		Role:  RolePrimary,
	}

	l := &Links{Version: LinksSchemaVersion, Links: []Link{orphan}}
	if removed := PruneDead(root, l); len(removed) != 0 {
		t.Fatalf("a missing workitem must not auto-prune, removed %+v", removed)
	}
}

func TestReportPruned_NamesEachRowAndTheUndo(t *testing.T) {
	var out strings.Builder
	ReportPruned(&out, []Pruned{{
		Link: Link{
			ID: "lnk_20260726_eeeeee", WorkitemID: "design-x-2026-07-26",
			Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/gone"},
		},
		Reason: "campaign_path workflow/design/gone no longer exists",
	}})
	got := out.String()
	for _, want := range []string{"lnk_20260726_eeeeee", "workflow/design/gone", "no longer exists", "undo:", "git checkout"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report %q missing %q", got, want)
		}
	}

	var quiet strings.Builder
	ReportPruned(&quiet, nil)
	if quiet.String() != "" {
		t.Fatalf("expected no output for an empty prune, got %q", quiet.String())
	}
}
