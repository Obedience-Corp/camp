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
// tag. A ref is stored in a directory's .workitem marker, so only a
// directory-kind item with a stable id has somewhere to keep one; intents and
// festivals identify themselves from their own source document and have no
// marker to write to. Callers use this to avoid promising a WI- segment that
// EnsureRefForCommit will not produce.
func CarriesCommitRef(wi *WorkItem) bool {
	return wi != nil && wi.ItemKind == ItemKindDirectory && wi.StableID != ""
}

// WorktreeLinkCommitNote is the one-line note printed after a worktree is
// primary-linked to wi, describing what a commit from inside it will actually
// carry. Only a ref-carrying workitem gets a WI- segment; for the rest the link
// still supplies workitem context, and saying so beats promising a tag that
// never appears.
func WorktreeLinkCommitNote(wi *WorkItem) string {
	if CarriesCommitRef(wi) {
		return "camp p commit in this worktree will include WI-* in the campaign tag"
	}
	return "camp p commit in this worktree will resolve to this workitem " +
		"(no WI-* segment: only adopted workitems carry a ref)"
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
		return "", camperrors.Wrap(err, "load campaign config for ref backfill")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := Discover(ctx, root, resolver)
	if err != nil {
		return "", camperrors.Wrap(err, "discover for ref collision set")
	}
	existing := RefsFromWorkitems(items)
	ref, err := DeriveUnique(ctx, wi.StableID, existing)
	if err != nil {
		_, _ = fmt.Fprintf(errw,
			"warning: could not derive ref for %s: %v; committing without WI segment\n",
			wi.RelativePath, err)
		return "", nil
	}
	if err := BackfillRef(ctx, root, wi.RelativePath, ref); err != nil {
		_, _ = fmt.Fprintf(errw,
			"warning: could not backfill ref for %s: %v; committing without WI segment\n",
			wi.RelativePath, err)
		return "", nil
	}
	_, _ = fmt.Fprintf(errw,
		"warning: backfilled missing ref for %s -> %s; commit the .workitem update with your next change\n",
		wi.RelativePath, ref)
	return ref, nil
}
