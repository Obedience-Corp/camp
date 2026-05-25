package workflow

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
)

func computeCreatePlan(campaignRoot string, cfg *config.CampaignConfig, opts createOptions) (*createPlan, error) {
	title := opts.Title
	if title == "" {
		title = opts.Type
	}

	relPath := path.Join("workflow", opts.Type) + "/"
	absPath := filepath.Join(campaignRoot, filepath.FromSlash(relPath))

	plan := &createPlan{
		Type:        opts.Type,
		Title:       title,
		WorkflowDir: absPath,
		WorkflowRel: relPath,
	}

	if _, err := os.Stat(absPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, camperrors.Wrap(err, "stat workflow directory")
		}
		plan.WorkflowDirCreate = true
	}

	for _, sub := range statusDirs {
		dir := filepath.Join(absPath, filepath.FromSlash(sub))
		if _, err := os.Stat(dir); err != nil {
			if !os.IsNotExist(err) {
				return nil, camperrors.Wrapf(err, "stat status dir %s", sub)
			}
			plan.MissingStatusDirs = append(plan.MissingStatusDirs, sub)
			plan.MissingGitKeeps = append(plan.MissingGitKeeps, sub)
			continue
		}
		gitkeep := filepath.Join(dir, ".gitkeep")
		if _, err := os.Stat(gitkeep); err != nil {
			if !os.IsNotExist(err) {
				return nil, camperrors.Wrapf(err, "stat .gitkeep in %s", sub)
			}
			plan.MissingGitKeeps = append(plan.MissingGitKeeps, sub)
		}
	}

	obeyPath := filepath.Join(absPath, "OBEY.md")
	if _, err := os.Stat(obeyPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, camperrors.Wrap(err, "stat workflow OBEY.md")
		}
		plan.OBEYWrite = true
	}

	if err := planShortcut(cfg, plan, opts); err != nil {
		return nil, err
	}
	if err := planConcept(cfg, plan, opts); err != nil {
		return nil, err
	}

	plan.NoChanges = !plan.WorkflowDirCreate &&
		len(plan.MissingStatusDirs) == 0 &&
		len(plan.MissingGitKeeps) == 0 &&
		!plan.OBEYWrite &&
		plan.Shortcut.NoChange &&
		plan.Concept.NoChange &&
		len(plan.Replaced) == 0

	return plan, nil
}

func planShortcut(cfg *config.CampaignConfig, plan *createPlan, opts createOptions) error {
	shortcutKey := nav.NormalizeNavigationName(opts.Shortcut)
	plan.Shortcut.Key = shortcutKey
	plan.Shortcut.Path = plan.WorkflowRel

	shortcuts := map[string]config.ShortcutConfig{}
	if cfg.Jumps != nil && cfg.Jumps.Shortcuts != nil {
		shortcuts = cfg.Jumps.Shortcuts
	}

	matches := matchingShortcutKeys(shortcuts, shortcutKey)
	for _, match := range matches {
		existing := shortcuts[match]
		if existing.Path != plan.WorkflowRel {
			if !opts.Replace {
				return camperrors.NewValidation("shortcut",
					"shortcut "+shortcutKey+" already points to "+existing.Path+"; use --replace to update it", nil)
			}
			plan.Shortcut.Replaced = true
			if plan.Shortcut.Existing == "" {
				plan.Shortcut.Existing = existing.Path
			}
		}
		if match != shortcutKey {
			plan.Replaced = append(plan.Replaced, match)
		}
	}

	// NoChange iff a single entry already exists at the normalized key with the
	// target path, and no case-variant cleanup is needed.
	existing, hasKey := shortcuts[shortcutKey]
	plan.Shortcut.NoChange = hasKey &&
		existing.Path == plan.WorkflowRel &&
		!plan.Shortcut.Replaced &&
		len(plan.Replaced) == 0

	sort.Strings(plan.Replaced)
	return nil
}

func planConcept(cfg *config.CampaignConfig, plan *createPlan, opts createOptions) error {
	plan.Concept.Name = opts.Type
	plan.Concept.Path = plan.WorkflowRel

	concepts := cfg.ConceptList
	if len(concepts) == 0 {
		concepts = cfg.Concepts()
	}

	for _, concept := range concepts {
		if !strings.EqualFold(concept.Name, opts.Type) {
			continue
		}
		if concept.Path == plan.WorkflowRel {
			plan.Concept.NoChange = true
			return nil
		}
		if !opts.Replace {
			return camperrors.NewValidation("type",
				"concept "+opts.Type+" already points to "+concept.Path+"; use --replace to update it", nil)
		}
		plan.Concept.Replaced = true
		plan.Concept.Existing = concept.Path
		return nil
	}
	return nil
}

func matchingShortcutKeys(shortcuts map[string]config.ShortcutConfig, shortcut string) []string {
	normalized := nav.NormalizeNavigationName(shortcut)
	var matches []string
	for key := range shortcuts {
		if nav.NormalizeNavigationName(key) == normalized {
			matches = append(matches, key)
		}
	}
	sort.Strings(matches)
	return matches
}
