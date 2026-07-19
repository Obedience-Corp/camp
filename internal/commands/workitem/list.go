package workitem

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

type listOptions struct {
	json            bool
	types           []string
	categories      []string
	statuses        []string
	stages          []string
	attentionStages []string
	groups          []string
	tags            []string
	projects        []string
	groupBy         string
	showParked      bool
	limit           int
	query           string
}

func (o listOptions) filterOptions() (wkitem.FilterOptions, error) {
	tags, err := normalizeTags(o.tags)
	if err != nil {
		return wkitem.FilterOptions{}, err
	}
	projects, err := normalizeProjects(o.projects)
	if err != nil {
		return wkitem.FilterOptions{}, err
	}
	return wkitem.FilterOptions{
		Types:           o.types,
		Categories:      o.categories,
		Statuses:        o.statuses,
		LifecycleStages: o.stages,
		AttentionStages: o.attentionStages,
		Groups:          o.groups,
		Tags:            tags,
		Projects:        projects,
		Query:           o.query,
		ShowParked:      o.showParked,
	}, nil
}

func newListCommand() *cobra.Command {
	var opts listOptions
	cmd := &cobra.Command{
		Use:   "list [type|status|category]",
		Short: "List or browse filtered workitems",
		Long: `List campaign workitems with the same filters used by the dashboard.

In a terminal, this opens the TUI with visible, editable prefilters. When
stdout is not a terminal, it prints a compact grouped list. Use --json for the
stable machine-readable contract in either environment.

The optional positional filter resolves as a workflow type, displayed status,
or configured category. Ambiguous values must use an explicit flag.

Examples:
  camp workitem list intent
  camp workitem list active
  camp workitem list --category research --query auth
  camp workitem list --tag public-launch --tag schema
  camp workitem list festival --status ready --json`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-TTY compact output and --json are safe for scripts and agents",
		},
		RunE: jsoncontract.RunE(wkitem.SchemaVersion, func() bool { return opts.json }, func(cmd *cobra.Command, args []string) error {
			state, err := discoverWorkitems(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if err := applyPositionalFilter(args[0], state, &opts); err != nil {
					return err
				}
			}
			if err := validateFlags(opts.json, false, false, "", opts.types, opts.categories, opts.stages, opts.attentionStages, opts.groups, opts.groupBy); err != nil {
				return err
			}
			if err := validateDisplayStatuses(opts.statuses); err != nil {
				return err
			}
			filters, err := opts.filterOptions()
			if err != nil {
				return err
			}
			if isInteractive() && !opts.json {
				// --limit applies only to non-TTY / --json result size, not the TUI.
				return runTUIWithFilters(cmd.Context(), state, filters, false, "")
			}

			items := wkitem.FilterAdvanced(state.items, filters)
			if opts.limit > 0 && len(items) > opts.limit {
				items = items[:opts.limit]
			}
			groupBy := opts.groupBy
			if groupBy == "" {
				if opts.json {
					groupBy = "attention_stage"
				} else {
					groupBy = "group"
				}
			}
			if opts.json {
				return outputJSON(cmd.Context(), state.campaignRoot, state.cfg, items, groupBy)
			}
			return outputList(cmd.OutOrStdout(), items, groupBy)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(wkitem.SchemaVersion, func() bool { return opts.json }))

	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
	cmd.Flags().StringArrayVar(&opts.types, "type", nil, "Filter by workflow type (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.categories, "category", nil, "Filter by workflow category (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.statuses, "status", nil, "Filter by displayed status: current, next, active, parked, inbox, ready, plan, ritual, chains, none (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.stages, "stage", nil, "Filter by lifecycle stage (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.attentionStages, "attention-stage", nil, "Filter by attention stage (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.groups, "group", nil, "Filter by workitem group (repeat for OR)")
	cmd.Flags().StringArrayVar(&opts.tags, "tag", nil, "Filter by tag (repeat; item must have ALL given tags)")
	cmd.Flags().StringArrayVar(&opts.projects, "project", nil, "Filter by related project (repeat for OR)")
	cmd.Flags().StringVar(&opts.groupBy, "group-by", "", "Group output sections by attention_stage, group, type, or category")
	cmd.Flags().BoolVar(&opts.showParked, "show-parked", false, "Include parked attention-stage workitems")
	cmd.Flags().IntVar(&opts.limit, "limit", 0, "Maximum number of items to return (non-interactive / --json only)")
	cmd.Flags().StringVar(&opts.query, "query", "", "Search query to filter items")
	return cmd
}

func applyPositionalFilter(raw string, state *discoveredWorkitems, opts *listOptions) error {
	value := strings.TrimSpace(raw)
	normalized := strings.ToLower(value)
	types := map[string]string{
		string(wkitem.WorkflowTypeIntent): string(wkitem.WorkflowTypeIntent), string(wkitem.WorkflowTypeDesign): string(wkitem.WorkflowTypeDesign),
		string(wkitem.WorkflowTypeExplore): string(wkitem.WorkflowTypeExplore), string(wkitem.WorkflowTypeFestival): string(wkitem.WorkflowTypeFestival),
	}
	for _, item := range state.items {
		workflowType := string(item.WorkflowType)
		types[strings.ToLower(workflowType)] = workflowType
	}
	categories := map[string]string{
		config.WorkflowCategoryPlan: config.WorkflowCategoryPlan, config.WorkflowCategoryResearch: config.WorkflowCategoryResearch,
		config.WorkflowCategoryPipeline: config.WorkflowCategoryPipeline, config.WorkflowCategoryReview: config.WorkflowCategoryReview,
		"uncategorized": "uncategorized",
	}
	for _, category := range categoryVocabulary(state.cfg) {
		categories[strings.ToLower(category.Key)] = category.Key
	}
	for _, item := range state.items {
		if item.WorkflowCategory != "" {
			categories[strings.ToLower(item.WorkflowCategory)] = item.WorkflowCategory
		}
	}

	status := wkitem.NormalizeDisplayStatus(normalized)
	var dimensions []string
	if _, ok := types[normalized]; ok {
		dimensions = append(dimensions, "type")
	}
	if _, ok := categories[normalized]; ok {
		dimensions = append(dimensions, "category")
	}
	if wkitem.IsDisplayStatus(status) {
		dimensions = append(dimensions, "status")
	}
	if len(dimensions) == 0 {
		return camperrors.NewValidation("filter", fmt.Sprintf("unknown workitem filter %q; use --type, --status, or --category", raw), nil)
	}
	if len(dimensions) > 1 {
		// Name the flags so callers (especially always-ambiguous "plan") know
		// which dimension to pin without re-reading the help text.
		flagHints := make([]string, 0, len(dimensions))
		for _, d := range dimensions {
			flagHints = append(flagHints, "--"+d+" "+value)
		}
		return camperrors.NewValidation("filter", fmt.Sprintf(
			"ambiguous workitem filter %q matches %s; use %s",
			raw, strings.Join(dimensions, " and "), strings.Join(flagHints, " or ")), nil)
	}
	switch dimensions[0] {
	case "type":
		opts.types = append(opts.types, types[normalized])
	case "category":
		opts.categories = append(opts.categories, categories[normalized])
	case "status":
		opts.statuses = append(opts.statuses, status)
	}
	return nil
}
