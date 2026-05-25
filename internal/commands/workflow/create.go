package workflow

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

	if !opts.DryRun && !plan.NoChanges {
		if err := applyCreatePlan(ctx, cmd, campaignRoot, cfg, plan); err != nil {
			return err
		}
	}

	return emitCreateResult(cmd, plan, opts)
}

func validatePathSegment(field, value string) error {
	return pathsafe.ValidateSegment(field, value)
}
