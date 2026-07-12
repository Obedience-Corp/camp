package workitem

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

type gatherOptions struct {
	Title    string
	Slug     string
	Force    bool
	DryRun   bool
	NoCommit bool
	JSON     bool
}

type workitemGatherResult struct {
	SchemaVersion string                     `json:"schema_version"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	DryRun        bool                       `json:"dry_run,omitempty"`
	Gathered      workitemGatherResultItem   `json:"gathered"`
	Sources       []workitemGatherResultMove `json:"sources"`
	Committed     bool                       `json:"committed"`
	CommitMessage string                     `json:"commit_message,omitempty"`
	Warnings      []string                   `json:"warnings,omitempty"`
}

type workitemGatherResultItem struct {
	ID           string `json:"id,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	RelativePath string `json:"relative_path"`
}

type workitemGatherResultMove struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug"`
	From string `json:"from"`
	To   string `json:"to"`
}

// NewGatherCommand creates a `camp gather <type>` subcommand that combines
// user-selected directory workitems of the given workflow type into one
// gathered package. Unlike `camp intent gather`, which merges file contents,
// this moves each source directory inside the new package so nothing is
// rewritten or lost.
func NewGatherCommand(workflowType string) *cobra.Command {
	var (
		title    string
		slug     string
		force    bool
		dryRun   bool
		noCommit bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   workflowType + " [selectors...]",
		Short: "Combine selected " + workflowType + " workitems into one gathered package",
		Long: `Combine selected ` + workflowType + ` workitems into one gathered package.

Sources are always chosen explicitly: pass 2 or more selectors (stable id,
key, path, or directory slug), or run with no arguments in a terminal for an
interactive picker. There is no automatic discovery mode.

The gather process:
  1. Create workflow/` + workflowType + `/<slug>/ with a fresh .workitem and a
     generated README.md indexing the gathered packages
  2. Move each source directory inside the new package (git history follows
     the rename)
  3. Stamp gathered_into/gathered_at on each source .workitem
  4. Migrate manual priority state and re-home workitem links
  5. Commit the move (unless --no-commit)

Moved sources stop appearing as separate workitems because discovery only
scans the top level of workflow/` + workflowType + `/.

Examples:
  camp gather ` + workflowType + ` pkg-one pkg-two --title "Unified topic"
  camp gather ` + workflowType + ` pkg-one pkg-two pkg-three -t "Unified topic" --slug unified-topic
  camp gather ` + workflowType + `                # interactive picker (TTY only)
  camp gather ` + workflowType + ` pkg-one pkg-two -t "Unified topic" --dry-run`,
		Args: jsoncontract.Args(WorkitemGatherJSONVersion, func() bool { return jsonOut }, cobra.ArbitraryArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by selectors and flags; only the bare invocation is interactive",
		},
		RunE: jsoncontract.RunE(WorkitemGatherJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runWorkitemGather(cmd, workflowType, args, gatherOptions{
				Title: title, Slug: slug, Force: force, DryRun: dryRun, NoCommit: noCommit, JSON: jsonOut,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemGatherJSONVersion, func() bool { return jsonOut }))

	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "Title for the gathered workitem (required unless prompted interactively)")
	f.StringVar(&slug, "slug", "", "Directory slug override (default: derived from title)")
	f.BoolVar(&force, "force", false, "Gather sources even when one has an active workflow run")
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned gather, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runWorkitemGather(cmd *cobra.Command, wfType string, args []string, opts gatherOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	rootDir, err := gatherRootDir(resolver, wfType)
	if err != nil {
		return err
	}

	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return camperrors.Wrap(err, "discovering work items")
	}
	var typed []wkitem.WorkItem
	for _, item := range items {
		if item.WorkflowType == wkitem.WorkflowType(wfType) && item.ItemKind == wkitem.ItemKindDirectory {
			typed = append(typed, item)
		}
	}

	title := strings.TrimSpace(opts.Title)
	var selected []wkitem.WorkItem
	if len(args) > 0 {
		for _, arg := range args {
			match, matchErr := matchGatherSelector(typed, arg)
			if matchErr != nil {
				return camperrors.Wrapf(matchErr, "resolving %s workitem", wfType)
			}
			selected = append(selected, *match)
		}
		selected = dedupeGatherItems(selected)
	} else {
		if opts.JSON || !isInteractive() {
			return camperrors.NewValidation("selectors",
				"pass at least 2 "+wfType+" workitem selectors (list candidates with `camp workitem --json --type "+wfType+"`)", nil)
		}
		selected, title, err = promptGatherSelection(typed, wfType, title)
		if err != nil {
			return err
		}
	}

	if len(selected) < 2 {
		return camperrors.NewValidation("selectors",
			fmt.Sprintf("need at least 2 %s workitems to gather, got %d", wfType, len(selected)), nil)
	}
	if title == "" {
		return camperrors.NewValidation("title", "--title is required", nil)
	}

	slugValue := opts.Slug
	if slugValue == "" {
		slugValue = intent.SlugFromTitle(title)
	}
	if err := validateSlug(slugValue); err != nil {
		return camperrors.NewValidation("slug", "invalid slug "+slugValue+": "+err.Error(), nil)
	}

	targetAbs := filepath.Join(rootDir, slugValue)
	if _, statErr := os.Stat(targetAbs); statErr == nil {
		return camperrors.NewValidation("slug",
			"target directory already exists: "+targetAbs+" (pass --slug to choose another)", nil)
	} else if !os.IsNotExist(statErr) {
		return camperrors.Wrapf(statErr, "stat %s", targetAbs)
	}
	targetRel, err := filepath.Rel(root, targetAbs)
	if err != nil {
		return camperrors.Wrapf(err, "resolving target path %s", targetAbs)
	}

	var warnings []string
	var blocked []string
	sources := make([]gatherSource, 0, len(selected))
	for _, item := range selected {
		abs := item.AbsPath(root)
		run, runErr := wkitem.LoadLocalRun(ctx, abs)
		if runErr != nil {
			warnings = append(warnings, fmt.Sprintf("workflow runtime unreadable for %s: %v", filepath.Base(item.RelativePath), runErr))
		} else if gatherBlockedByRun(run) {
			blocked = append(blocked, filepath.Base(item.RelativePath))
		}
		meta, metaErr := wkitem.LoadMetadata(ctx, abs)
		if metaErr != nil {
			warnings = append(warnings, fmt.Sprintf(".workitem unreadable for %s: %v", filepath.Base(item.RelativePath), metaErr))
			meta = nil
		}
		sources = append(sources, gatherSource{Item: item, Meta: meta})
	}
	if len(blocked) > 0 && !opts.Force {
		return camperrors.NewValidation("sources",
			"active workflow run in: "+strings.Join(blocked, ", ")+" (finish or abandon the run, or pass --force)", nil)
	}

	plan := gatherPlan{
		WorkflowType: wfType,
		Title:        title,
		Slug:         slugValue,
		TargetAbs:    targetAbs,
		TargetRel:    filepath.ToSlash(targetRel),
		Sources:      sources,
	}

	if opts.DryRun {
		return emitGatherDryRun(cmd, plan, opts.JSON)
	}

	execution, err := executeGather(ctx, cmd, cfg, root, plan, opts, warnings)
	if err != nil {
		return err
	}
	return emitGatherResult(cmd, plan, execution, opts.JSON)
}

func promptGatherSelection(typed []wkitem.WorkItem, wfType, preTitle string) ([]wkitem.WorkItem, string, error) {
	if len(typed) < 2 {
		return nil, "", camperrors.NewValidation("selectors",
			fmt.Sprintf("need at least 2 %s workitems to gather, found %d", wfType, len(typed)), nil)
	}

	byPath := make(map[string]wkitem.WorkItem, len(typed))
	options := make([]huh.Option[string], 0, len(typed))
	for _, item := range typed {
		byPath[item.RelativePath] = item
		base := filepath.Base(item.RelativePath)
		label := strings.TrimSpace(item.Title)
		if label == "" || strings.EqualFold(label, base) {
			label = base
		} else {
			label = label + " (" + base + ")"
		}
		options = append(options, huh.NewOption(label, item.RelativePath))
	}

	var selectedPaths []string
	title := preTitle
	fields := []huh.Field{
		huh.NewMultiSelect[string]().
			Title("Select " + wfType + " workitems to gather").
			Description("Space toggles, enter confirms; pick at least 2").
			Options(options...).
			Validate(func(v []string) error {
				if len(v) < 2 {
					return camperrors.NewValidation("selectors", "select at least 2 workitems", nil)
				}
				return nil
			}).
			Value(&selectedPaths),
	}
	if strings.TrimSpace(title) == "" {
		fields = append(fields, huh.NewInput().
			Title("Title for the gathered workitem").
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return camperrors.NewValidation("title", "title is required", nil)
				}
				return nil
			}).
			Value(&title))
	}

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return nil, "", camperrors.Wrap(err, "gather selection cancelled")
	}

	selected := make([]wkitem.WorkItem, 0, len(selectedPaths))
	for _, p := range selectedPaths {
		selected = append(selected, byPath[p])
	}
	return dedupeGatherItems(selected), strings.TrimSpace(title), nil
}

func newGatherResult(plan gatherPlan) workitemGatherResult {
	result := workitemGatherResult{
		SchemaVersion: WorkitemGatherJSONVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	result.Gathered = workitemGatherResultItem{
		Type:         plan.WorkflowType,
		Title:        plan.Title,
		RelativePath: plan.TargetRel,
	}
	for _, src := range plan.Sources {
		base := filepath.Base(src.Item.RelativePath)
		move := workitemGatherResultMove{
			Slug: base,
			From: filepath.ToSlash(src.Item.RelativePath),
			To:   plan.TargetRel + "/" + base,
		}
		if src.Meta != nil {
			move.ID = src.Meta.ID
		}
		result.Sources = append(result.Sources, move)
	}
	return result
}

func emitGatherJSON(cmd *cobra.Command, result workitemGatherResult) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return camperrors.Wrap(err, "encoding JSON output")
	}
	return nil
}

func emitGatherDryRun(cmd *cobra.Command, plan gatherPlan, jsonOut bool) error {
	if jsonOut {
		result := newGatherResult(plan)
		result.DryRun = true
		return emitGatherJSON(cmd, result)
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "dry-run: would gather %d %s workitems into %s\n", len(plan.Sources), plan.WorkflowType, plan.TargetRel)
	for _, src := range plan.Sources {
		base := filepath.Base(src.Item.RelativePath)
		_, _ = fmt.Fprintf(out, "  %s → %s/%s\n", filepath.ToSlash(src.Item.RelativePath), plan.TargetRel, base)
	}
	return nil
}

func emitGatherResult(cmd *cobra.Command, plan gatherPlan, execution *gatherExecution, jsonOut bool) error {
	if jsonOut {
		result := newGatherResult(plan)
		result.Gathered.ID = execution.ID
		result.Gathered.Ref = execution.Ref
		result.Sources = execution.Moves
		result.Committed = execution.Committed
		result.CommitMessage = execution.CommitMessage
		result.Warnings = execution.Warnings
		return emitGatherJSON(cmd, result)
	}

	for _, w := range execution.Warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.WarningIcon(), w)
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s Gathered %d %s workitems into %s\n", ui.SuccessIcon(), len(execution.Moves), plan.WorkflowType, plan.TargetRel)
	for _, move := range execution.Moves {
		_, _ = fmt.Fprintf(out, "  %s → %s\n", move.From, move.To)
	}
	_, err := fmt.Fprintf(out, "  id: %s\n  ref: %s\n", execution.ID, execution.Ref)
	return err
}
