//go:build container_fs

package links

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// AttachPrimary writes links.yaml, so these run inside the pooled container
// rather than on a developer's machine (decision D007).

// AttachPrimary is the writer behind every worktree-creating command. A
// worktree scope must leave it carrying the project that owns the worktree,
// whether the caller named the project or not.
func TestAttachPrimary_RecordsTheOwningProject(t *testing.T) {
	ctx := context.Background()
	const wtRel = "projects/worktrees/fest/demo"

	cases := []struct {
		name        string
		scope       LinkScope
		wantProject string
		why         string
	}{
		{
			name:        "derived when the caller does not supply one",
			scope:       LinkScope{Kind: ScopeWorktree, Path: wtRel},
			wantProject: "projects/fest",
			why:         "the path already names the project, so nothing should be lost",
		},
		{
			name:        "the caller's project is kept",
			scope:       LinkScope{Kind: ScopeWorktree, Path: wtRel, Project: "projects/camp-timeline"},
			wantProject: "projects/camp-timeline",
			why:         "worktree creation knows the project better than the path does",
		},
		{
			name:  "a project scope gains nothing",
			scope: LinkScope{Kind: ScopeProject, Path: "projects/fest"},
			why:   "a project scope already is its project",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
				t.Fatal(err)
			}
			link, err := AttachPrimary(ctx, root, AttachOptions{
				WorkitemID:   "design-example-2026-09-07",
				Scope:        tc.scope,
				CreatedBy:    "test",
				AllowMissing: true,
			})
			if err != nil {
				t.Fatalf("AttachPrimary: %v", err)
			}
			if link.Scope.Project != tc.wantProject {
				t.Fatalf("scope.project = %q, want %q (%s)", link.Scope.Project, tc.wantProject, tc.why)
			}
			reloaded, err := Load(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if got := reloaded.Links[0].Scope.Project; got != tc.wantProject {
				t.Fatalf("persisted scope.project = %q, want %q", got, tc.wantProject)
			}
		})
	}
}

// AttachPrimary is I/O; a cancelled context must stop it before it writes.
func TestAttachPrimary_CancelledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID:   "design-example-2026-09-07",
		Scope:        LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/fest/demo"},
		CreatedBy:    "test",
		AllowMissing: true,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(LinksPath(root)); !os.IsNotExist(err) {
		t.Fatal("a cancelled attach must not have written links.yaml")
	}
}
