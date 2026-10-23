package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/version"
)

// RepairSkillCategory groups repair changes for shipped skill files.
const RepairSkillCategory = "skill"

const campaignRecipePath = "campaign/scaffold.yaml"

// skillsBackupGitignoreRule keeps the copies a forced skills update makes
// outside git, under .campaign/skills/.backup/, out of commits.
const skillsBackupGitignoreRule = intskills.SkillsSubdir + "/" + intskills.BackupDirName + "/"

// ShippedSkills renders the skill bundles this camp version ships, with the
// template vars of the camp at absDir, through the scaffold engine init uses.
func ShippedSkills(ctx context.Context, absDir string) ([]intskills.ShippedFile, error) {
	files, err := intskills.RenderShipped(ctx, embeddedScaffoldFS(), campaignRecipePath, shippedSkillVars(ctx, absDir))
	if err != nil {
		return nil, camperrors.Wrap(err, "render shipped skills")
	}
	return files, nil
}

func shippedSkillVars(ctx context.Context, absDir string) map[string]any {
	vars := map[string]any{
		"campaign_name": filepath.Base(absDir),
		"Profile":       version.Profile,
		"now":           time.Now().UTC().Format(time.RFC3339),
	}
	cfg, err := config.LoadCampaignConfig(ctx, absDir)
	if err != nil {
		return vars
	}
	if cfg.Name != "" {
		vars["campaign_name"] = cfg.Name
	}
	vars["campaign_id"] = cfg.ID
	vars["campaign_type"] = string(cfg.Type)
	vars["campaign_description"] = cfg.Description
	vars["campaign_mission"] = cfg.Mission
	return vars
}

// HasSkillNotices reports whether the plan preserves shipped skill files that
// differ from this camp version, which repair reports but never applies.
func (p *RepairPlan) HasSkillNotices() bool {
	return slices.ContainsFunc(p.Changes, func(c RepairChange) bool {
		return c.Category == RepairSkillCategory && c.Type == RepairPreserve
	})
}

func computeSkillChanges(ctx context.Context, absDir string, plan *RepairPlan) error {
	shipped, err := ShippedSkills(ctx, absDir)
	if err != nil {
		return err
	}
	inv, err := intskills.ClassifyCamp(ctx, absDir, shipped)
	if err != nil {
		return camperrors.Wrap(err, "classify shipped skills")
	}

	seen := repairSeenKeys(plan)
	for _, f := range inv.Files {
		key := f.RelPath()
		if seen[key] {
			continue
		}
		change := RepairChange{Category: RepairSkillCategory, Key: key}
		switch f.State {
		case intskills.ShippedOutdated:
			change.Type = RepairModify
			change.Description = "outdated shipped skill, update to this camp version"
			if f.GitEvidence {
				change.Description = "unchanged since camp wrote it per git history, update to this camp version"
			}
		case intskills.ShippedModified:
			change.Type = RepairPreserve
			change.Description = fmt.Sprintf("modified locally, preserved; run camp skills update --force %s to replace", f.Bundle)
		case intskills.ShippedUnknown:
			change.Type = RepairPreserve
			change.Description = fmt.Sprintf("modified or outdated, run camp skills update --force %s", f.Bundle)
		case intskills.ShippedSymlink:
			change.Type = RepairPreserve
			change.Description = "symlink, left alone; camp never replaces symlinked skill files"
		default:
			continue
		}
		plan.Changes = append(plan.Changes, change)
	}

	if inv.ManifestStale() {
		change := RepairChange{
			Type:        RepairModify,
			Category:    RepairSkillCategory,
			Key:         manifestRelPath(),
			Description: "record shipped skill hashes",
		}
		if inv.Manifest == nil {
			change.Type = RepairAdd
		}
		plan.Changes = append(plan.Changes, change)
	}
	return nil
}

func updateShippedSkills(ctx context.Context, absDir string, result *InitResult) error {
	shipped, err := ShippedSkills(ctx, absDir)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(skillsDir(absDir), intskills.ManifestFileName)
	_, statErr := os.Stat(manifestPath)
	hadManifest := statErr == nil

	res, err := intskills.Update(ctx, intskills.UpdateOptions{
		Root:        absDir,
		Shipped:     shipped,
		CampVersion: version.Version,
	})
	if err != nil {
		return camperrors.Wrap(err, "update shipped skills")
	}
	for _, out := range res.Files {
		if out.Written {
			result.FilesModified = append(result.FilesModified, filepath.Join(absDir, filepath.FromSlash(out.RelPath())))
		}
	}
	if !res.ManifestWritten {
		return nil
	}
	if hadManifest {
		result.FilesModified = append(result.FilesModified, manifestPath)
	} else {
		result.FilesCreated = append(result.FilesCreated, manifestPath)
	}
	return nil
}

func skillsDir(absDir string) string {
	return filepath.Join(absDir, config.CampaignDir, intskills.SkillsSubdir)
}

func manifestRelPath() string {
	return config.CampaignDir + "/" + intskills.SkillsSubdir + "/" + intskills.ManifestFileName
}
