package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func reportNothingToApply(plan *scaffold.RepairPlan, w Writers) {
	if !plan.HasSkillNotices() {
		writeLine(w.HumanOut, ui.Success("Your camp is up to date, nothing to repair."))
		return
	}
	printRepairDiff(plan, w)
	writeLine(w.HumanOut)
	writeLine(w.HumanOut, ui.Warning("No automatic repairs: the skill files above differ from what this camp version ships and were preserved."))
}

// skillsNeedProjection reports whether any registered tool is missing one or
// more campaign skill projections (so repair has skills work to do). It is a
// best-effort check: on any error it returns false rather than forcing repair.
func skillsNeedProjection(root string) bool {
	skillsDir := filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)
	slugs, err := intskills.DiscoverSkillSlugs(skillsDir)
	if err != nil || len(slugs) == 0 {
		return false
	}
	for _, tool := range intskills.ToolNames() {
		relPath, err := intskills.ResolveToolPath(tool)
		if err != nil {
			continue
		}
		dest := filepath.Join(root, relPath)
		state, err := intskills.InspectSkillProjection(dest, skillsDir, slugs)
		if err != nil {
			continue
		}
		if state.Linked < state.TotalSkills {
			return true
		}
	}
	return false
}

// projectCampaignSkills links campaign skill bundles into every registered tool
// directory under root. It is best-effort: failures are returned as human-readable
// warnings rather than aborting the init flow. It returns the tools that were
// fully linked, any warnings to surface, and the campaign-root-relative paths of
// the skill links that were created or replaced (so the caller can stage exactly
// those paths in a selective repair commit).
func projectCampaignSkills(root string) (linked []string, warnings []string, projectedPaths []string) {
	skillsDir := filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		return nil, nil, nil
	}

	results, err := intskills.LinkDefaultTools(root, skillsDir, false, false, os.Stderr)
	if err != nil {
		return nil, []string{fmt.Sprintf("skills not linked: %v", err)}, nil
	}

	for _, res := range results {
		if res.Err != nil {
			warnings = append(warnings, fmt.Sprintf("%s skills not linked: %v", res.Tool, res.Err))
			continue
		}
		if res.Summary.Conflicts > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %d conflicting path(s) left untouched (%s); run 'camp skills status'",
				res.Tool, res.Summary.Conflicts, strings.Join(res.Summary.ConflictNames, ", ")))
		}
		relPath, err := intskills.ResolveToolPath(res.Tool)
		if err == nil {
			for _, slug := range res.Summary.CreatedNames {
				projectedPaths = append(projectedPaths, filepath.Join(relPath, slug))
			}
			for _, slug := range res.Summary.ReplacedNames {
				projectedPaths = append(projectedPaths, filepath.Join(relPath, slug))
			}
		}
		linked = append(linked, res.Tool)
	}
	return linked, warnings, projectedPaths
}
