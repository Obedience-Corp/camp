package links

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// AttachOptions configures AttachPrimary.
type AttachOptions struct {
	// WorkitemID is the stable workitem identity stored on the link
	// (prefer StableID / .workitem id).
	WorkitemID string
	// WorkitemKey is the discoverable key (e.g. design:workflow/design/foo).
	WorkitemKey string
	// Scope is the target (worktree, project, festival, …).
	Scope LinkScope
	// CreatedBy stamps the link; empty defaults to "camp".
	CreatedBy string
	// Replace replaces an existing primary on the same scope.
	Replace bool
	// AllowMissing skips ValidateLinkPath existence checks (migrations).
	AllowMissing bool
	// Report receives a line for each dead row the write pruned. Never nil in
	// command code: pruning a row the user did not ask about is only acceptable
	// because it is reported.
	Report io.Writer
}

// AttachPrimary records a primary workitem→scope link in links.yaml.
// This is the shared writer used by `camp workitem link` and by
// `camp project worktree add --workitem` so worktree creation can attach a
// design/explore/… workitem and `camp p commit` in that worktree inherits WI-*.
func AttachPrimary(ctx context.Context, campaignRoot string, opts AttachOptions) (Link, error) {
	if opts.WorkitemID == "" {
		return Link{}, camperrors.NewValidation("workitem_id", "workitem id is required", nil)
	}
	if opts.Scope.Kind == "" || opts.Scope.Path == "" {
		return Link{}, camperrors.NewValidation("scope", "scope kind and path are required", nil)
	}
	if !opts.AllowMissing {
		if err := quest.ValidateLinkPath(campaignRoot, opts.Scope.Path); err != nil {
			return Link{}, camperrors.Wrap(err, "scope path")
		}
	}
	createdBy := opts.CreatedBy
	if createdBy == "" {
		createdBy = "camp"
	}

	var out Link
	var pruned []Pruned
	err := WithLock(ctx, campaignRoot, func(registry *Links) error {
		// Drop rows whose scope target is provably gone while the registry is
		// open anyway. See Dead for why this is the only signal trusted here.
		//
		// A caller with nowhere to report to does not prune. Reporting is the
		// price of removing a row the user did not ask about, so a missing
		// Report writer makes the removal silent -- and silence is the one
		// outcome this must not have. Skipping leaves the rows for the next
		// reporting caller or for doctor.
		if opts.Report != nil {
			pruned = PruneDead(campaignRoot, registry)
		}

		id, idErr := NewLinkID(registry)
		if idErr != nil {
			return idErr
		}
		out = Link{
			ID:          id,
			WorkitemID:  opts.WorkitemID,
			WorkitemKey: opts.WorkitemKey,
			Scope:       opts.Scope,
			Role:        RolePrimary,
			CreatedAt:   time.Now().UTC().Truncate(time.Second),
			CreatedBy:   createdBy,
		}
		if err := registry.AddLink(out, opts.Replace); err != nil {
			return err
		}
		return AsError(ValidateOne(ctx, registry, out.ID, ValidateOptions{
			CampaignRoot: campaignRoot,
			AllowMissing: opts.AllowMissing,
			Now:          out.CreatedAt,
		}))
	})
	if err != nil {
		return Link{}, err
	}
	ReportPruned(opts.Report, pruned)
	return out, nil
}

// ReportPruned writes one line per dropped row plus the undo, so a user who did
// not ask for the cleanup can see exactly what went, why, and how to put it
// back. Naming the reason matters: a worktree link dropped because its workitem
// is gone otherwise reads as camp deleting worktree links.
func ReportPruned(w io.Writer, pruned []Pruned) {
	if w == nil || len(pruned) == 0 {
		return
	}
	for _, p := range pruned {
		_, _ = fmt.Fprintf(w, "removed dead link %s (%s:%s): %s\n",
			p.Link.ID, p.Link.Scope.Kind, p.Link.Scope.Path, p.Reason)
	}
	// The prune and the caller's own write land in one save, so the undo is
	// not surgical. Say so: a user who runs it expecting to keep the link they
	// just made would otherwise lose it silently, which is the same failure
	// this reporting exists to prevent.
	_, _ = fmt.Fprintf(w,
		"  undo: git checkout -- .campaign/workitems/links.yaml (reverts this whole write, new link included)\n")
}

// WorktreeScopePath returns the campaign-relative path for a project worktree
// at projects/worktrees/<project>/<name>.
func WorktreeScopePath(project, name string) string {
	return "projects/worktrees/" + project + "/" + name
}

// NewLinkID returns a fresh lnk_YYYYMMDD_<6 hex> ID that does not collide with
// any existing entry in registry.
func NewLinkID(registry *Links) (string, error) {
	existing := make(map[string]struct{}, len(registry.Links))
	for _, l := range registry.Links {
		existing[l.ID] = struct{}{}
	}
	const maxAttempts = 32
	for i := 0; i < maxAttempts; i++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", camperrors.Wrap(err, "generate link id: read random bytes")
		}
		candidate := fmt.Sprintf("lnk_%s_%02x%02x%02x",
			time.Now().UTC().Format("20060102"), b[0], b[1], b[2])
		if _, clash := existing[candidate]; !clash {
			return candidate, nil
		}
	}
	return "", camperrors.New(fmt.Sprintf("generate link id: %d-attempt collision retry exhausted", maxAttempts))
}
