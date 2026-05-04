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
)

// Options controls Attach behavior.
type Options struct {
	// Force overwrites an existing marker at the target.
	Force bool
}

// Result describes the outcome of a successful attach.
type Result struct {
	// Input is the path the user passed (for display purposes).
	Input string
	// Target is the resolved directory where the marker was written.
	Target string
	// CampaignID is the campaign the target is now attached to.
	CampaignID string
	// FollowedSymlink is true when Input != Target.
	FollowedSymlink bool
}

// Attach writes a Kind="attachment" marker at the resolved target of input,
// binding it to the given campaign.
//
// Refuses if:
//   - input does not resolve.
//   - target is already inside a campaign root.
//   - target already has a marker (unless opts.Force).
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

	if isUnderCampaignRoot(target, campaignRoot) {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
			"target %q is already inside a campaign tree; attach is for external directories", target)
	}

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

	marker := campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		Kind:             campaign.KindAttachment,
		ActiveCampaignID: campaignID,
	}
	if err := campaign.WriteMarker(target, marker); err != nil {
		return nil, camperrors.Wrapf(err, "write marker at %q", markerPath)
	}

	return &Result{
		Input:           input,
		Target:          target,
		CampaignID:      campaignID,
		FollowedSymlink: target != abs,
	}, nil
}

// Detach removes the attachment marker at the resolved target of input.
// Refuses if the marker is missing or has Kind="project".
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

	return &Result{
		Input:           input,
		Target:          target,
		CampaignID:      marker.EffectiveCampaignID(),
		FollowedSymlink: target != abs,
	}, nil
}

func isUnderCampaignRoot(target, campaignRoot string) bool {
	if campaignRoot == "" {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		resolvedRoot = campaignRoot
	}
	rel, err := filepath.Rel(resolvedRoot, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !startsWithDotDot(rel))
}

func startsWithDotDot(p string) bool {
	if len(p) >= 2 && p[0] == '.' && p[1] == '.' {
		if len(p) == 2 {
			return true
		}
		return p[2] == '/' || p[2] == filepath.Separator
	}
	return false
}
