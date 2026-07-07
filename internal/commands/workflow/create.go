package workflow

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// JSONSchemaVersion is the contract version for `camp workflow` JSON output.
const JSONSchemaVersion = "workflow/v1"

// terminalDungeonDirs are the terminal archive directories scaffolded inside
// each camp workflow collection. Active work remains ergonomic at
// workflow/<type>/<item>; camp does not create intent-style live buckets.
var terminalDungeonDirs = []string{
	"dungeon/completed",
	"dungeon/archived",
	"dungeon/someday",
}

func newCreateCommand() *cobra.Command {
	var shortcut, title, category string
	var replace, dryRun, jsonOut bool

	cmd := &cobra.Command{
		Use:   "create <type>",
		Short: "Create a custom workflow collection",
		Long: `Create a custom workflow collection under workflow/<type>/.

The command creates the workflow directory, terminal dungeon directories,
.gitkeep files, and an OBEY.md guide, then registers the collection in
campaign configuration through a concept and navigation shortcut. A shortcut is
required. Use --dry-run to inspect planned writes and --json for
machine-readable planning or apply results.`,
		Args: jsoncontract.Args(JSONSchemaVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		RunE: jsoncontract.RunE(JSONSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), cmd, createOptions{
				Type:     args[0],
				Shortcut: shortcut,
				Title:    title,
				Category: category,
				Replace:  replace,
				DryRun:   dryRun,
				JSON:     jsonOut,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JSONSchemaVersion, func() bool { return jsonOut }))

	cmd.Flags().StringVar(&shortcut, "shortcut", "", "navigation shortcut for this workflow")
	cmd.Flags().StringVar(&title, "title", "", "human-readable workflow title")
	cmd.Flags().StringVar(&category, "category", "", "workflow category for filtering (default plan; must exist under workflows.categories in campaign.yaml)")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing shortcut or concept with the same name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report planned writes without modifying the filesystem")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")

	return cmd
}

type createOptions struct {
	Type     string
	Shortcut string
	Title    string
	Category string
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

// categoryPlan describes the planned category_by_type mutation.
type categoryPlan struct {
	Category string `json:"category"`
	Existing string `json:"existing,omitempty"`
	NoChange bool   `json:"no_change"`
}

// conceptPlan describes the planned mutation to the campaign concept.
type conceptPlan struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Parent   string `json:"parent,omitempty"` // parent concept the new collection nests under
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

	WorkflowDirCreate   bool     // workflow/<type>/ does not yet exist
	MissingScaffoldDirs []string // subset of terminalDungeonDirs that do not yet exist
	MissingGitKeeps     []string // scaffold dirs (from terminalDungeonDirs) whose .gitkeep is missing
	OBEYWrite           bool     // OBEY.md does not yet exist

	Shortcut shortcutPlan
	Concept  conceptPlan
	Category categoryPlan

	Replaced  []string // shortcut keys removed under --replace (e.g. case variants)
	NoChanges bool     // every action would be a no-op
}

func runCreate(ctx context.Context, cmd *cobra.Command, opts createOptions) error {
	if err := validatePathSegment("type", opts.Type); err != nil {
		return err
	}
	if opts.Shortcut == "" {
		return camperrors.NewValidation("shortcut", "shortcut is required (use --shortcut <key>)", nil)
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
	return pathutil.ValidateSegment(field, value)
}
