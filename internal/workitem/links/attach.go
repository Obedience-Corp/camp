package links

import (
	"context"
	"crypto/rand"
	"fmt"
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
	err := WithLock(ctx, campaignRoot, func(registry *Links) error {
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
		if errs := Validate(ctx, registry, ValidateOptions{
			CampaignRoot: campaignRoot,
			AllowMissing: opts.AllowMissing,
			Now:          out.CreatedAt,
		}); len(errs) > 0 {
			return camperrors.NewValidation(errs[0].Field, errs[0].Message, nil)
		}
		return nil
	})
	if err != nil {
		return Link{}, err
	}
	return out, nil
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
