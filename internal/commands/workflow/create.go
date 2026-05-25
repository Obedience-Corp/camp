package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/pathsafe"
)

// JSONSchemaVersion is the contract version for `camp workflow` JSON output.
const JSONSchemaVersion = "workflow/v1"

// statusDirs are the status sub-directories scaffolded inside every workflow
// collection. They mirror the workitem-collection layout used by
// `.campaign/intents/` and are documented in DESIGN.md §3.1.
var statusDirs = []string{
	"inbox",
	"active",
	"ready",
	"dungeon/completed",
	"dungeon/archived",
	"dungeon/someday",
}

func newCreateCommand() *cobra.Command {
	var shortcut, title string
	var replace, dryRun, jsonOut bool

	cmd := &cobra.Command{
		Use:   "create <type>",
		Short: "Create a custom workflow collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), cmd, createOptions{
				Type:     args[0],
				Shortcut: shortcut,
				Title:    title,
				Replace:  replace,
				DryRun:   dryRun,
				JSON:     jsonOut,
			})
		},
	}

	cmd.Flags().StringVar(&shortcut, "shortcut", "", "navigation shortcut for this workflow")
	cmd.Flags().StringVar(&title, "title", "", "human-readable workflow title")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing shortcut or concept with the same name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report planned writes without modifying the filesystem")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	_ = cmd.MarkFlagRequired("shortcut")

	return cmd
}

type createOptions struct {
	Type     string
	Shortcut string
	Title    string
	Replace  bool
	DryRun   bool
	JSON     bool
}

// shortcutPlan describes the planned mutation to the navigation shortcut.
type shortcutPlan struct {
	Key      string `json:"key"`
	Path     string `json:"path"`
	Existing string `json:"existing,omitempty"`
	Replaced bool   `json:"replaced"`
	NoChange bool   `json:"no_change"`
}

// conceptPlan describes the planned mutation to the campaign concept.
type conceptPlan struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Existing string `json:"existing,omitempty"`
	Replaced bool   `json:"replaced"`
	NoChange bool   `json:"no_change"`
}

// createPlan is the read-only result of computeCreatePlan.
type createPlan struct {
	Type        string
	Title       string
	WorkflowDir string // absolute
	WorkflowRel string // relative to campaign root, with trailing slash

	WorkflowDirCreate bool     // workflow/<type>/ does not yet exist
	MissingStatusDirs []string // subset of statusDirs that do not yet exist
	MissingGitKeeps   []string // status dirs (from statusDirs) whose .gitkeep is missing
	OBEYWrite         bool     // OBEY.md does not yet exist

	Shortcut shortcutPlan
	Concept  conceptPlan

	Replaced  []string // shortcut keys removed under --replace (e.g. case variants)
	NoChanges bool     // every action would be a no-op
}

func runCreate(ctx context.Context, cmd *cobra.Command, opts createOptions) error {
	if err := validatePathSegment("type", opts.Type); err != nil {
		return err
	}
	if err := validatePathSegment("shortcut", opts.Shortcut); err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	plan, err := computeCreatePlan(campaignRoot, cfg, opts)
	if err != nil {
		return err
	}

	if !opts.DryRun {
		if err := applyCreatePlan(ctx, cmd, campaignRoot, cfg, plan); err != nil {
			return err
		}
	}

	return emitCreateResult(cmd, plan, opts)
}

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

func applyCreatePlan(ctx context.Context, cmd *cobra.Command, campaignRoot string, cfg *config.CampaignConfig, plan *createPlan) error {
	if err := os.MkdirAll(plan.WorkflowDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create workflow directory %s", plan.WorkflowRel)
	}

	if err := writeStatusScaffold(plan); err != nil {
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
	invalidateNavigationCache(cmd, campaignRoot)
	return nil
}

func writeStatusScaffold(plan *createPlan) error {
	for _, sub := range statusDirs {
		dir := filepath.Join(plan.WorkflowDir, filepath.FromSlash(sub))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return camperrors.Wrapf(err, "create status dir %s", sub)
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

func emitCreateResult(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	if opts.JSON {
		return emitCreateJSON(cmd, plan, opts)
	}
	return emitCreateHuman(cmd, plan, opts)
}

func emitCreateHuman(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	w := cmd.OutOrStdout()
	if plan.NoChanges {
		fmt.Fprintf(w, "no changes for workflow %s\n", plan.Type)
		return nil
	}

	if opts.DryRun {
		fmt.Fprintf(w, "plan: create %s\n", strings.TrimRight(plan.WorkflowRel, "/"))
	} else {
		fmt.Fprintf(w, "created %s\n", strings.TrimRight(plan.WorkflowRel, "/"))
	}
	fmt.Fprintf(w, "  shortcut: %s -> %s\n", plan.Shortcut.Key, plan.WorkflowRel)
	fmt.Fprintf(w, "  workitem type: %s\n", plan.Type)
	fmt.Fprintf(w, "  status dirs: %s\n", strings.Join(statusDirsForOutput(), ", "))
	fmt.Fprintf(w, "next: camp workitem create <slug> --type %s\n", plan.Type)
	if opts.DryRun {
		fmt.Fprintln(w, "dry run: nothing written. re-run without --dry-run to apply.")
	}
	return nil
}

func emitCreateJSON(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	payload := struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Type          string       `json:"type"`
		Title         string       `json:"title"`
		WorkflowDir   string       `json:"workflow_dir"`
		StatusDirs    []string     `json:"status_dirs"`
		OBEYWritten   bool         `json:"obey_written"`
		Shortcut      shortcutPlan `json:"shortcut"`
		Concept       conceptPlan  `json:"concept"`
		Replaced      []string     `json:"replaced"`
		NoChanges     bool         `json:"no_changes"`
		DryRun        bool         `json:"dry_run"`
		Applied       bool         `json:"applied"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Type:          plan.Type,
		Title:         plan.Title,
		WorkflowDir:   plan.WorkflowRel,
		StatusDirs:    statusDirsForOutput(),
		OBEYWritten:   plan.OBEYWrite && !opts.DryRun,
		Shortcut:      plan.Shortcut,
		Concept:       plan.Concept,
		Replaced:      append([]string(nil), plan.Replaced...),
		NoChanges:     plan.NoChanges,
		DryRun:        opts.DryRun,
		Applied:       !opts.DryRun,
	}
	if payload.Replaced == nil {
		payload.Replaced = []string{}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func statusDirsForOutput() []string {
	out := make([]string, len(statusDirs))
	for i, sub := range statusDirs {
		out[i] = sub + "/"
	}
	return out
}

func validatePathSegment(field, value string) error {
	return pathsafe.ValidateSegment(field, value)
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

func upsertConcept(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, name, relPath, title string, replace bool) error {
	concepts := cfg.ConceptList
	if len(concepts) == 0 {
		concepts = cfg.Concepts()
	}

	for i, concept := range concepts {
		if strings.EqualFold(concept.Name, name) {
			if concept.Path == relPath {
				cfg.ConceptList = concepts
				return nil
			}
			if !replace {
				return camperrors.NewValidation("type",
					"concept "+name+" already points to "+concept.Path+"; use --replace to update it", nil)
			}
			concepts[i] = config.ConceptEntry{
				Name:        name,
				Path:        relPath,
				Description: title + " workflow",
			}
			cfg.ConceptList = concepts
			return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
		}
	}

	concepts = append(concepts, config.ConceptEntry{
		Name:        name,
		Path:        relPath,
		Description: title + " workflow",
	})
	cfg.ConceptList = concepts
	return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
}

func invalidateNavigationCache(cmd *cobra.Command, campaignRoot string) {
	if err := navindex.Delete(campaignRoot); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to invalidate navigation cache: %v\n", err)
	}
}
