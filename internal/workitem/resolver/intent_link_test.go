package resolver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

const resolverIntentID = "intents-cannot-be-primary-linked-20260803-184438"

// writeIntentLinkedWorktree lays down a campaign with one intent, a worktree
// directory, and a primary worktree link that addresses the intent by the given
// workitem_id/workitem_key pair. It returns the campaign root and the absolute
// worktree path.
func writeIntentLinkedWorktree(t *testing.T, workitemID, workitemKey string) (root, worktree string) {
	t.Helper()
	root = t.TempDir()
	writeMinimalCampaign(t, root)

	intentRel := ".campaign/intents/inbox/" + resolverIntentID + ".md"
	body := "---\nid: " + resolverIntentID +
		"\ntitle: Intents cannot be primary-linked\nstatus: inbox" +
		"\ncreated_at: 2026-08-03T18:44:38Z\ntype: bug\n---\n\n# Intent\n"
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(intentRel)), body)

	worktree = filepath.Join(root, "projects", "worktrees", "camp", "intent-links")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}

	registry := `version: workitem-links/v1alpha1
links:
  - id: lnk_20260803_aaaaaa
    workitem_id: ` + workitemID + `
    workitem_key: ` + workitemKey + `
    scope:
      kind: worktree
      path: projects/worktrees/camp/intent-links
    role: primary
    created_at: 2026-08-03T18:44:38Z
    created_by: camp_workitem_link
`
	mustWriteFile(t, filepath.Join(root, ".campaign", "workitems", "links.yaml"), registry)
	return root, worktree
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolve_IntentWorktreeLinkResolvesFromInsideTheWorktree is the round-trip
// the fix exists for: the bare id a link stores for an intent must resolve back
// to that intent from inside the linked worktree, which is how camp p commit
// picks up workitem context there.
func TestResolve_IntentWorktreeLinkResolvesFromInsideTheWorktree(t *testing.T) {
	intentKey := "intent:.campaign/intents/inbox/" + resolverIntentID + ".md"

	tests := []struct {
		name        string
		workitemID  string
		workitemKey string
	}{
		{"bare frontmatter id", resolverIntentID, intentKey},
		{"id written without a key", resolverIntentID, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, worktree := writeIntentLinkedWorktree(t, tt.workitemID, tt.workitemKey)

			got, err := Resolve(context.Background(), root, Options{Cwd: worktree})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Source != SourceLink {
				t.Fatalf("Source = %q, want %q; trace=%+v", got.Source, SourceLink, got.Trace)
			}
			if got.Workitem == nil {
				t.Fatal("Workitem is nil; the worktree link did not resolve")
			}
			if got.Workitem.SourceID != resolverIntentID {
				t.Fatalf("resolved source_id = %q, want %q", got.Workitem.SourceID, resolverIntentID)
			}
			if got.Workitem.WorkflowType != workitem.WorkflowTypeIntent {
				t.Fatalf("resolved workflow_type = %q, want intent", got.Workitem.WorkflowType)
			}
		})
	}
}
