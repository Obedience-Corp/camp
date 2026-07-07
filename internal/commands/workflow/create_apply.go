package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

// applyCreatePlan is intentionally not transactional: if a later write fails,
// earlier filesystem/config writes remain. The navigation cache is invalidated
// on every exit so follow-up commands rebuild after partial success.
func applyCreatePlan(ctx context.Context, cmd *cobra.Command, campaignRoot string, cfg *config.CampaignConfig, plan *createPlan) (err error) {
	defer invalidateNavigationCache(cmd, campaignRoot)

	if err := os.MkdirAll(plan.WorkflowDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create workflow directory %s", plan.WorkflowRel)
	}

	if err := writeWorkflowScaffold(plan); err != nil {
		return err
	}

	if err := writeOBEYIfMissing(plan.WorkflowDir, plan.Type, plan.Title); err != nil {
		return err
	}

	if err := upsertShortcut(ctx, campaignRoot, cfg, plan.Shortcut.Key, plan.WorkflowRel, plan.Title, plan.Shortcut.Replaced); err != nil {
		return err
	}
	if err := upsertConcept(ctx, campaignRoot, cfg, plan.Type, plan.WorkflowRel, plan.Title, plan.Concept.Replaced); err != nil {
		return err
	}
	if !plan.Category.NoChange {
		if err := upsertCategoryMapping(ctx, campaignRoot, cfg, plan.Type, plan.Category.Category); err != nil {
			return err
		}
	}
	return nil
}

func upsertCategoryMapping(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, workflowType, category string) error {
	if cfg.Workflows.CategoryByType == nil {
		cfg.Workflows.CategoryByType = make(map[string]string)
	}
	cfg.Workflows.CategoryByType[workflowType] = category
	return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
}

func writeWorkflowScaffold(plan *createPlan) error {
	for _, sub := range terminalDungeonDirs {
		dir := filepath.Join(plan.WorkflowDir, filepath.FromSlash(sub))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return camperrors.Wrapf(err, "create scaffold dir %s", sub)
		}
		gitkeep := filepath.Join(dir, ".gitkeep")
		if _, err := os.Stat(gitkeep); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "stat .gitkeep in %s", sub)
		}
		if err := os.WriteFile(gitkeep, nil, 0o644); err != nil {
			return camperrors.Wrapf(err, "write .gitkeep in %s", sub)
		}
	}
	return nil
}

func writeOBEYIfMissing(absPath, workflowType, title string) error {
	obeyPath := filepath.Join(absPath, "OBEY.md")
	if _, err := os.Stat(obeyPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return camperrors.Wrap(err, "stat workflow OBEY.md")
	}

	content := fmt.Sprintf(`# %s

Custom workflow collection for %q workitems.

Create a workitem:

`+"```bash"+`
camp workitem create <slug> --type %s
`+"```"+`
`, title, workflowType, workflowType)

	if err := os.WriteFile(obeyPath, []byte(content), 0o644); err != nil {
		return camperrors.Wrap(err, "write workflow OBEY.md")
	}
	return nil
}

func upsertShortcut(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, shortcut, relPath, title string, replace bool) error {
	jumps := cfg.Jumps
	if jumps == nil {
		defaults := config.DefaultJumpsConfig()
		jumps = &defaults
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}

	shortcutKey := nav.NormalizeNavigationName(shortcut)
	matches := matchingShortcutKeys(jumps.Shortcuts, shortcutKey)
	for _, match := range matches {
		existing := jumps.Shortcuts[match]
		if existing.Path != relPath && !replace {
			return camperrors.NewValidation("shortcut",
				"shortcut "+shortcutKey+" already points to "+existing.Path+"; use --replace to update it", nil)
		}
	}
	for _, match := range matches {
		if match != shortcutKey {
			delete(jumps.Shortcuts, match)
		}
	}

	jumps.Shortcuts[shortcutKey] = config.ShortcutConfig{
		Path:        relPath,
		Description: title + " workflow",
		Source:      config.ShortcutSourceUser,
	}
	cfg.Jumps = jumps

	if err := config.SaveJumpsConfig(ctx, campaignRoot, jumps); err != nil {
		return err
	}
	return nil
}

// workflowParentName is the concept that groups all workflow collections.
const workflowParentName = "workflow"

// flatWorkflowConceptIndex finds a legacy top-level workflow concept of the
// given name, or -1. These predate the nested shape and survive until init
// --repair, so create folds one in rather than registering a duplicate child.
func flatWorkflowConceptIndex(concepts []config.ConceptEntry, name string) int {
	for i := range concepts {
		if strings.EqualFold(concepts[i].Name, workflowParentName) {
			continue
		}
		if strings.EqualFold(concepts[i].Name, name) && strings.HasPrefix(concepts[i].Path, "workflow/") {
			return i
		}
	}
	return -1
}

func upsertConcept(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, name, relPath, title string, replace bool) error {
	concepts := cfg.ConceptList
	if len(concepts) == 0 {
		concepts = cfg.Concepts()
	}

	// Drop a legacy flat top-level workflow concept with this name so it does not
	// linger beside the nested child registered below.
	migrated := false
	if idx := flatWorkflowConceptIndex(concepts, name); idx != -1 {
		concepts = append(concepts[:idx], concepts[idx+1:]...)
		migrated = true
	}

	// Find the workflow parent, creating it if absent.
	parentIdx := -1
	for i := range concepts {
		if strings.EqualFold(concepts[i].Name, workflowParentName) {
			parentIdx = i
			break
		}
	}
	if parentIdx == -1 {
		concepts = append(concepts, config.ConceptEntry{
			Name:        workflowParentName,
			Path:        "workflow/",
			Description: "Workflows",
		})
		parentIdx = len(concepts) - 1
	}

	child := config.ConceptEntry{Name: name, Path: relPath, Description: title + " workflow"}
	children := concepts[parentIdx].Children
	for j := range children {
		if !strings.EqualFold(children[j].Name, name) {
			continue
		}
		if children[j].Path == relPath {
			concepts[parentIdx].Children = children
			cfg.ConceptList = concepts
			if migrated {
				return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
			}
			return nil
		}
		if !replace {
			return camperrors.NewValidation("type",
				"concept "+name+" already points to "+children[j].Path+"; use --replace to update it", nil)
		}
		children[j] = child
		concepts[parentIdx].Children = children
		cfg.ConceptList = concepts
		return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
	}

	concepts[parentIdx].Children = append(children, child)
	cfg.ConceptList = concepts
	return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
}

func invalidateNavigationCache(cmd *cobra.Command, campaignRoot string) {
	if err := navindex.Delete(campaignRoot); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to invalidate navigation cache: %v\n", err)
	}
}
