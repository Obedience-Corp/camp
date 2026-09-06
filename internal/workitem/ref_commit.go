package workitem

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// RefOf reads the workitem ref off SourceMetadata.
func RefOf(wi *WorkItem) string {
	if wi == nil || wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata["ref"].(string); ok {
		return v
	}
	return ""
}

// CarriesCommitRef reports whether wi can carry a WI- ref in a campaign commit
// tag. Directory workitems store a ref in their .workitem marker; intents store
// one in their own frontmatter. Festivals declare identity in fest.yaml, which
// this path does not write, so they are excluded. Callers use this to avoid
// promising a WI- segment that EnsureRefForCommit will not produce.
func CarriesCommitRef(wi *WorkItem) bool {
	if wi == nil {
		return false
	}
	if wi.ItemKind == ItemKindDirectory {
		return wi.StableID != ""
	}
	return wi.WorkflowType == WorkflowTypeIntent && wi.SourceID != ""
}

// WorktreeLinkCommitNote is the one-line note printed after a worktree is
// primary-linked to wi, describing what a commit from inside it will actually
// carry. Only a ref-carrying workitem gets a WI- segment; for the rest the link
// still supplies workitem context, and saying so beats promising a tag that
// never appears.
func WorktreeLinkCommitNote(wi *WorkItem) string {
	if CarriesCommitRef(wi) {
		return "camp p commit in this worktree will include WI-* in the camp tag"
	}
	return "camp p commit in this worktree will resolve to this workitem " +
		"(no WI-* segment: festivals and workitems without a stable id carry no ref)"
}

// EnsureRefForCommit returns the workitem's ref, auto-backfilling the
// .workitem marker on disk if the field is empty and the workitem can carry a
// ref at all (see CarriesCommitRef). Intents/festivals and unresolved
// workitems return "" with no side effect. Failures during backfill fall
// back to "" with a louder stderr warning so the commit can still proceed
// without a WI- segment.
func EnsureRefForCommit(ctx context.Context, root string, wi *WorkItem, errw io.Writer) (string, error) {
	if !CarriesCommitRef(wi) {
		return "", nil
	}
	if v := RefOf(wi); v != "" {
		return v, nil
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", camperrors.Wrap(err, "load camp config for ref backfill")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := Discover(ctx, root, resolver)
	if err != nil {
		return "", camperrors.Wrap(err, "discover for ref collision set")
	}
	existing := RefsFromWorkitems(items)
	workitemID := LinkWorkitemID(wi)
	ref, err := DeriveUnique(ctx, workitemID, existing)
	if err != nil {
		_, _ = fmt.Fprintf(errw,
			"warning: could not derive ref for %s: %v; committing without WI segment\n",
			wi.RelativePath, err)
		return "", nil
	}
	writeRef := BackfillRef
	if wi.ItemKind == ItemKindFile {
		writeRef = BackfillIntentRef
	}
	if err := writeRef(ctx, root, wi.RelativePath, ref); err != nil {
		_, _ = fmt.Fprintf(errw,
			"warning: could not backfill ref for %s: %v; committing without WI segment\n",
			wi.RelativePath, err)
		return "", nil
	}
	backfillNoun := "the .workitem update"
	if wi.ItemKind == ItemKindFile {
		backfillNoun = "the intent file"
	}
	_, _ = fmt.Fprintf(errw,
		"warning: backfilled missing ref for %s -> %s; commit %s with your next change\n",
		wi.RelativePath, ref, backfillNoun)
	return ref, nil
}
