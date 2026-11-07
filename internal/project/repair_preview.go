package project

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// RepairCandidate is an existing checkout that may replace a broken link.
// Name matching is discovery only, never proof that two repositories match.
type RepairCandidate struct {
	Path        string `json:"path"`
	Origin      string `json:"origin,omitempty"`
	OriginKey   string `json:"originKey,omitempty"`
	OwnerID     string `json:"ownerId,omitempty"`
	OwnerName   string `json:"ownerName,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
}

// RepairPreview is read-only evidence for an explicit link replacement.
type RepairPreview struct {
	Name          string            `json:"name"`
	Path          string            `json:"path"`
	OldTarget     string            `json:"oldTarget"`
	TrackedChange bool              `json:"trackedChange"`
	Candidates    []RepairCandidate `json:"candidates"`
	Selected      *RepairCandidate  `json:"selected,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
}

func PreviewLinkRepair(ctx context.Context, campaignRoot, name, selectedPath string) (*RepairPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimPrefix(strings.TrimSpace(name), "projects/")
	if err := ValidateProjectName(name); err != nil {
		return nil, err
	}
	root, err := normalizeCampaignRoot(campaignRoot)
	if err != nil {
		return nil, err
	}
	if _, err := config.ReadCampaignConfig(ctx, root); err != nil {
		return nil, camperrors.Wrap(err, "load camp config")
	}
	linkPath := filepath.Join(root, "projects", name)
	st, err := dirlink.Lstat(linkPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "inspect project link")
	}
	if !st.IsDirectoryLink() {
		return nil, camperrors.Wrapf(camperrors.ErrConflict, "projects/%s is not a directory link", name)
	}
	if _, err := os.Stat(linkPath); err == nil {
		return nil, camperrors.Wrapf(camperrors.ErrConflict, "projects/%s already resolves; repair only accepts broken links", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, camperrors.Wrapf(err, "inspect projects/%s target", name)
	}
	old, err := dirlink.ReadTarget(linkPath)
	if err != nil {
		return nil, err
	}
	tracked, err := repairLinkTracked(ctx, root, filepath.ToSlash(filepath.Join("projects", name)))
	if err != nil {
		return nil, err
	}
	preview := &RepairPreview{Name: name, Path: filepath.ToSlash(filepath.Join("projects", name)), OldTarget: old, TrackedChange: tracked, Candidates: []RepairCandidate{}}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load registered camps")
	}
	seen := map[string]bool{}
	for _, registered := range reg.ListAll() {
		path := filepath.Join(registered.Path, "projects", name)
		candidate, err := inspectRepairCandidate(ctx, path, reg)
		if err != nil {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("inspect registered candidate %s: %v", path, err))
			continue
		}
		if candidate == nil {
			continue
		}
		if !seen[candidate.Path] {
			preview.Candidates = append(preview.Candidates, *candidate)
			seen[candidate.Path] = true
		}
	}
	if selectedPath != "" {
		candidate, err := inspectRepairCandidate(ctx, pathutil.ExpandHome(selectedPath), reg)
		if err != nil {
			return nil, err
		}
		if candidate == nil {
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "candidate %s is not a Git checkout root", selectedPath)
		}
		preview.Selected = candidate
		if !seen[candidate.Path] {
			preview.Candidates = append(preview.Candidates, *candidate)
		}
	}
	sort.Slice(preview.Candidates, func(i, j int) bool { return preview.Candidates[i].Path < preview.Candidates[j].Path })
	sort.Strings(preview.Warnings)
	return preview, nil
}

func inspectRepairCandidate(ctx context.Context, path string, reg *config.Registry) (*RepairCandidate, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve candidate %s", path)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	root, err := git.IsWorktreeRoot(ctx, resolved)
	if err != nil {
		return nil, err
	}
	if !root {
		return nil, nil
	}
	candidate := &RepairCandidate{Path: resolved}
	if marker, err := campaign.ReadMarker(resolved); err == nil {
		candidate.OwnerID = marker.EffectiveCampaignID()
		candidate.ProjectName = marker.ProjectName
		if owner, ok := reg.GetByID(candidate.OwnerID); ok {
			candidate.OwnerName = owner.Name
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, camperrors.Wrap(err, "read candidate ownership")
	}
	candidate.Origin, err = git.RemoteOriginURL(ctx, resolved)
	if err != nil {
		return nil, err
	}
	candidate.OriginKey = NormalizeRepairOrigin(candidate.Origin)
	return candidate, nil
}

// NormalizeRepairOrigin compares common SSH and HTTPS spellings of one repo.
func NormalizeRepairOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") && strings.Contains(raw, ":") && !filepath.IsAbs(raw) {
		parts := strings.SplitN(raw, ":", 2)
		host := strings.TrimPrefix(parts[0], "git@")
		return strings.ToLower(host) + "/" + strings.TrimSuffix(strings.Trim(strings.ToLower(parts[1]), "/"), ".git")
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Hostname()) + "/" + strings.TrimSuffix(strings.Trim(strings.ToLower(parsed.Path), "/"), ".git")
	}
	return strings.TrimSuffix(strings.Trim(strings.ToLower(raw), "/"), ".git")
}

func repairLinkTracked(ctx context.Context, root, rel string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--stage", "--", rel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, camperrors.Wrapf(err, "inspect tracked project link %s: %s", rel, strings.TrimSpace(string(out)))
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// RepairSelectionHint explains why discovery never silently chooses a checkout.
func RepairSelectionHint(preview *RepairPreview) string {
	if preview.Selected != nil {
		return "selected path reviewed; apply requires explicit --apply"
	}
	if len(preview.Candidates) == 0 {
		return "no registered candidate found; pass --path /absolute/checkout to inspect one"
	}
	return fmt.Sprintf("%d possible checkout(s); pass --path /absolute/checkout to select one", len(preview.Candidates))
}
