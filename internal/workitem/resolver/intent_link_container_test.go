//go:build container_fs

package resolver

import (
	"context"
	"testing"
)

// These stage a campaign and a worktree on disk, so they run inside the pooled
// container rather than on a developer's machine (decision D007).

// Resolving inside a worktree must keep answering with the workitem, which is
// what stamps the WI-* tag on a commit made there, and must additionally name
// the project the worktree checks out. The worktree stays visible as the detail
// rather than standing in for the project.
func TestResolve_WorktreeLinkReportsTheOwningProject(t *testing.T) {
	intentKey := "intent:.campaign/intents/inbox/" + resolverIntentID + ".md"
	root, worktree := writeIntentLinkedWorktree(t, resolverIntentID, intentKey)

	got, err := Resolve(context.Background(), root, Options{Cwd: worktree})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Source != SourceLink || got.Workitem == nil {
		t.Fatalf("commit-tag routing regressed: source=%q workitem=%v trace=%+v",
			got.Source, got.Workitem, got.Trace)
	}
	if got.Project != "projects/camp" {
		t.Errorf("Project = %q, want projects/camp derived from the worktree path", got.Project)
	}
	if got.Worktree != "projects/worktrees/camp/intent-links" {
		t.Errorf("Worktree = %q, want the worktree kept as the detail", got.Worktree)
	}
}

// A match from a tier that has no link scope must not borrow one.
func TestResolve_NonLinkTierReportsNoProject(t *testing.T) {
	intentKey := "intent:.campaign/intents/inbox/" + resolverIntentID + ".md"
	root, _ := writeIntentLinkedWorktree(t, resolverIntentID, intentKey)

	got, err := Resolve(context.Background(), root, Options{Explicit: resolverIntentID})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Source != SourceExplicit {
		t.Fatalf("Source = %q, want %q", got.Source, SourceExplicit)
	}
	if got.Project != "" || got.Worktree != "" {
		t.Fatalf("Project=%q Worktree=%q; an explicit selector names no scope", got.Project, got.Worktree)
	}
}
