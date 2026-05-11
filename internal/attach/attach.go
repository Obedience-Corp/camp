// Package attach implements non-project attachment of arbitrary directories
// to a campaign via the .camp link marker (Kind="attachment").
//
// Unlike linked projects, attachments do not create a projects/<name> symlink
// and are not registered as projects. The user manages whatever symlink (if
// any) they use to reach the directory; this package only writes/removes the
// marker so context detection works from inside the attached target.
package attach

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// Options controls Attach behavior.
type Options struct {
	// Force overwrites an existing marker at the target.
	Force bool
}

// Result describes the outcome of a successful attach or detach.
type Result struct {
	// Input is the path the user passed (for display purposes).
	Input string
	// Target is the resolved directory where the marker was written.
	Target string
	// CampaignID is the campaign the target is now attached to.
	CampaignID string
	// FollowedSymlink is true when Input != Target.
	FollowedSymlink bool
	// GitExcludeUpdated is true when the operation also updated the
	// target repo's .git/info/exclude file.
	GitExcludeUpdated bool
	// GitExcludeWarning is non-empty when a best-effort exclude update
	// failed; the marker write/remove still succeeded.
	GitExcludeWarning string
}

// Attach writes a Kind="attachment" marker at the resolved target of input,
// binding it to the given campaign.
//
// Refuses if:
//   - input does not resolve.
//   - target is already inside any campaign tree (the selected one or
//     another). A nested marker would shadow the parent .campaign/ during
//     detection.
//   - target already has a marker (unless opts.Force).
//
// When the target is a Git working tree, .camp is added to that repo's
// .git/info/exclude on a best-effort basis so campaign-local state is not
// accidentally committed to the attached repo.
func Attach(ctx context.Context, campaignRoot, campaignID, input string, opts Options) (*Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if campaignID == "" {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "campaign ID required")
	}

	abs, err := filepath.Abs(input)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve %q", input)
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve symlinks for %q", input)
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, camperrors.Wrapf(err, "stat target %q", target)
	}
	if !info.IsDir() {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "attach target %q is not a directory", target)
	}

	// Check for an existing marker at target first.
	markerPath := campaign.MarkerPath(target)
	if existing, readErr := campaign.ReadMarker(target); readErr == nil {
		if !opts.Force {
			return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
				"marker already exists at %q (kind=%q); use --force to overwrite", markerPath, existing.Kind)
		}
		if existing.Kind == campaign.KindProject {
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
				"refusing to overwrite linked-project marker at %q with --force; use 'camp project unlink' first",
				markerPath)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) && !os.IsNotExist(readErr) {
		return nil, camperrors.Wrapf(readErr, "read existing marker at %q", markerPath)
	}

	// Walk from the parent so an attachment marker AT target itself is
	// invisible to Detect. This lets the any-campaign check run on the
	// --force re-attach path too: a target now inside another campaign
	// (e.g. moved under a new campaign root) would otherwise shadow it.
	if existingRoot, ok := detectExistingCampaign(ctx, filepath.Dir(target)); ok {
		switch {
		case sameCampaignRoot(existingRoot, campaignRoot):
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
				"target %q is already inside campaign root %q; attach is for external directories",
				target, existingRoot)
		default:
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
				"target %q is already inside a different campaign at %q; refusing to write a shadowing .camp marker",
				target, existingRoot)
		}
	}

	marker := campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		Kind:             campaign.KindAttachment,
		ActiveCampaignID: campaignID,
	}
	if err := campaign.WriteMarker(target, marker); err != nil {
		return nil, camperrors.Wrapf(err, "write marker at %q", markerPath)
	}

	res := &Result{
		Input:           input,
		Target:          target,
		CampaignID:      campaignID,
		FollowedSymlink: target != abs,
	}
	if git.IsRepo(target) {
		updated, err := git.EnsureInfoExclude(ctx, target, campaign.LinkMarkerFile)
		switch {
		case err != nil:
			res.GitExcludeWarning = err.Error()
		case updated:
			res.GitExcludeUpdated = true
		}
	}
	return res, nil
}

// Detach removes the attachment marker at the resolved target of input.
// Refuses if the marker is missing or has Kind="project".
//
// When the target is a Git working tree, .camp is removed from that repo's
// .git/info/exclude on a best-effort basis to mirror Attach.
func Detach(ctx context.Context, input string) (*Result, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	abs, err := filepath.Abs(input)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve %q", input)
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve symlinks for %q", input)
	}

	markerPath := campaign.MarkerPath(target)
	marker, err := campaign.ReadMarker(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, camperrors.Wrapf(camperrors.ErrNotFound, "no marker at %q", markerPath)
		}
		return nil, camperrors.Wrapf(err, "read marker at %q", markerPath)
	}
	if marker.Kind == campaign.KindProject {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
			"%q is a linked project; use 'camp project unlink' instead", target)
	}

	if err := campaign.RemoveMarker(target); err != nil {
		return nil, camperrors.Wrapf(err, "remove marker at %q", markerPath)
	}

	res := &Result{
		Input:           input,
		Target:          target,
		CampaignID:      marker.EffectiveCampaignID(),
		FollowedSymlink: target != abs,
	}
	if git.IsRepo(target) {
		updated, err := git.RemoveInfoExclude(ctx, target, campaign.LinkMarkerFile)
		switch {
		case err != nil:
			res.GitExcludeWarning = err.Error()
		case updated:
			res.GitExcludeUpdated = true
		}
	}
	return res, nil
}

// detectExistingCampaign returns the existing campaign root for target, if
// any. It uses the same Detect path as the rest of the CLI so attachments
// cannot shadow another campaign by writing a .camp marker inside it.
func detectExistingCampaign(ctx context.Context, target string) (string, bool) {
	root, err := campaign.Detect(ctx, target)
	if err != nil {
		return "", false
	}
	return root, true
}

func sameCampaignRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	resolveOrSelf := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return resolveOrSelf(a) == resolveOrSelf(b)
}
