package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	wktui "github.com/Obedience-Corp/camp/internal/workitem/tui"
)

// NewWorkitemCommand creates the camp workitem command.
func NewWorkitemCommand() *cobra.Command {
	var (
		flagJSON            bool
		flagList            bool
		flagPrint           bool
		flagPathOutput      string
		flagTypes           []string
		flagCategories      []string
		flagStages          []string
		flagAttentionStages []string
		flagGroups          []string
		flagGroupBy         string
		flagShowParked      bool
		flagLimit           int
		flagQuery           string
	)

	cmd := &cobra.Command{
		Use:     "workitem",
		Aliases: []string{"wi", "workitems"},
		Short:   "View active campaign work items",
		Long: `View active campaign work items across intents, designs, explore, and festivals.

Default mode launches an interactive TUI dashboard. Use --json for machine-readable
output or --print to select and print a path for shell integration.

Examples:
  camp workitem                              # interactive dashboard
  camp workitem --json                       # JSON output for agents/scripts
  camp workitem --json --type design         # filter by type
  camp workitem --json --type intent --limit 5
  camp workitem --print                      # select and print path`,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Supports --json for non-interactive output",
		},
		RunE: jsoncontract.RunE(wkitem.SchemaVersion, func() bool { return flagJSON }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := validateFlags(flagJSON, flagList, flagPrint, flagPathOutput, flagTypes, flagCategories, flagStages, flagAttentionStages, flagGroups, flagGroupBy); err != nil {
				return err
			}

			interactive := isInteractive()
			if !interactive && !flagJSON && !flagList && !flagPrint && flagPathOutput == "" {
				return camperrors.New("non-interactive use requires --json, --list, or --print flag")
			}

			cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}
			campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
			if err != nil {
				return camperrors.Wrap(err, "resolving campaign root")
			}
			resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

			items, err := wkitem.Discover(ctx, campaignRoot, resolver)
			if err != nil {
				return camperrors.Wrap(err, "discovering work items")
			}
			items = wkitem.ApplyWorkflowCategories(items, cfg.WorkflowCategoryForType)

			// Load priority store read-only; pruning happens only in explicit write paths.
			storePath := priority.StorePath(campaignRoot)
			store, err := priority.Load(storePath)
			if err != nil {
				return camperrors.Wrap(err, "loading priority store")
			}

			// Apply priority overlay and re-sort with priority buckets.
			items = priority.Apply(store, items)
			wkitem.Sort(items)

			items = wkitem.FilterAdvanced(items, wkitem.FilterOptions{
				Types:           flagTypes,
				Categories:      flagCategories,
				LifecycleStages: flagStages,
				AttentionStages: flagAttentionStages,
				Groups:          flagGroups,
				Query:           flagQuery,
				ShowParked:      flagShowParked,
			})
			if flagLimit > 0 && flagLimit < len(items) {
				items = items[:flagLimit]
			}

			displayGroupBy := flagGroupBy
			if flagList && !cmd.Flags().Changed("group-by") {
				displayGroupBy = "group"
			}

			switch {
			case flagList:
				return outputList(cmd.OutOrStdout(), items, displayGroupBy)
			case flagJSON:
				return outputJSON(campaignRoot, cfg, items, displayGroupBy)
			case !interactive:
				// Non-interactive --print/--path-output: output first item path directly.
				if len(items) == 0 {
					return camperrors.Newf("no work items found")
				}
				return outputSelectedPath(items[0], flagPrint, flagPathOutput)
			case flagPathOutput != "":
				return runTUI(ctx, items, false, flagPathOutput, campaignRoot, resolver, store, storePath, flagShowParked, cfg.WorkflowCategoryForType)
			case flagPrint:
				return runTUI(ctx, items, true, "", campaignRoot, resolver, store, storePath, flagShowParked, cfg.WorkflowCategoryForType)
			default:
				return runTUI(ctx, items, false, "", campaignRoot, resolver, store, storePath, flagShowParked, cfg.WorkflowCategoryForType)
			}
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(wkitem.SchemaVersion, func() bool { return flagJSON }))

	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&flagList, "list", false, "Output a compact grouped list")
	cmd.Flags().BoolVar(&flagPrint, "print", false, "Print path only (for shell integration)")
	cmd.Flags().StringVar(&flagPathOutput, "path-output", "", "Write selected relative path to file (shell integration)")
	_ = cmd.Flags().MarkHidden("path-output")
	cmd.Flags().StringArrayVar(&flagTypes, "type", nil, "Filter by workflow type (builtin: intent, design, explore, festival; or any slug-safe custom type produced by 'camp workitem create --type <name>')")
	cmd.Flags().StringArrayVar(&flagCategories, "category", nil, "Filter by workflow category (builtin: plan, research, pipeline, review, uncategorized; or any category defined under workflows in campaign.yaml)")
	cmd.Flags().StringArrayVar(&flagStages, "stage", nil, "Filter by lifecycle stage (none, inbox, active, ready, planning, ritual, chains)")
	cmd.Flags().StringArrayVar(&flagAttentionStages, "attention-stage", nil, "Filter by attention stage (current, next, active, parked)")
	cmd.Flags().StringArrayVar(&flagGroups, "group", nil, "Filter by workitem group")
	cmd.Flags().StringVar(&flagGroupBy, "group-by", "attention_stage", "Group JSON/list sections by attention_stage, group, type, or category; --list defaults to group unless set")
	cmd.Flags().BoolVar(&flagShowParked, "show-parked", false, "include parked attention-stage workitems in default output")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Maximum number of items to return")
	cmd.Flags().StringVar(&flagQuery, "query", "", "Search query to filter items")

	cmd.AddCommand(newCreateCommand())
	cmd.AddCommand(newAdoptCommand())
	cmd.AddCommand(newLinkCommand())
	cmd.AddCommand(newUnlinkCommand())
	cmd.AddCommand(newLinksCommand())
	cmd.AddCommand(newWorktreeCommand())
	cmd.AddCommand(newCurrentCommand())
	cmd.AddCommand(newResolveCommand())
	cmd.AddCommand(newPriorityCommand())
	cmd.AddCommand(newStageCommand())
	cmd.AddCommand(newGroupCommand())
	cmd.AddCommand(newPromoteCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newRepairCommand())
	cmd.AddCommand(newCommitCommand())
	cmd.AddCommand(newCommitsCommand())

	return cmd
}

func outputSelectedPath(item wkitem.WorkItem, printOnly bool, pathOutput string) error {
	path := selectedJumpPath(item)
	if pathOutput != "" {
		return os.WriteFile(pathOutput, []byte(path), 0o600)
	}
	if printOnly {
		fmt.Println(path)
		return nil
	}
	fmt.Printf("cd %s\n", path)
	return nil
}

type selectedAction string

const (
	selectedActionJumpDirectory selectedAction = "jump_directory"
	selectedActionOpenEditor    selectedAction = "open_editor"
)

func selectedDefaultAction(item wkitem.WorkItem) selectedAction {
	if item.ItemKind == wkitem.ItemKindFile {
		return selectedActionOpenEditor
	}
	return selectedActionJumpDirectory
}

func selectedJumpPath(item wkitem.WorkItem) string {
	if item.ItemKind == wkitem.ItemKindFile {
		return filepath.Dir(item.RelativePath)
	}
	return item.RelativePath
}

func selectedOpenPath(item wkitem.WorkItem, campaignRoot string) string {
	if item.PrimaryDoc != "" {
		return item.AbsPrimaryDoc(campaignRoot)
	}
	if item.RelativePath != "" {
		return item.AbsPath(campaignRoot)
	}
	return ""
}

func runSelectedAction(ctx context.Context, item wkitem.WorkItem, printOnly bool, pathOutput string, campaignRoot string) error {
	if printOnly {
		return outputSelectedPath(item, true, "")
	}
	if selectedDefaultAction(item) == selectedActionOpenEditor {
		return openSelectedItem(ctx, item, campaignRoot)
	}
	return outputSelectedPath(item, false, pathOutput)
}

func openSelectedItem(ctx context.Context, item wkitem.WorkItem, campaignRoot string) error {
	path := selectedOpenPath(item, campaignRoot)
	if path == "" {
		return camperrors.Newf("selected work item has no path to open")
	}
	editorName := editor.GetEditor(ctx)
	cmd := editor.BuildEditorCommand(ctx, editorName, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return camperrors.Wrap(err, "opening selected work item")
	}
	return nil
}

func isInteractive() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func validateFlags(jsonMode, listMode, printMode bool, pathOutput string, types, categories, stages, attentionStages, groups []string, groupBy string) error {
	for _, t := range types {
		if err := validateSlug(t); err != nil {
			return camperrors.NewValidation("type",
				fmt.Sprintf("invalid --type value %q: must be a path-safe workflow type (no '/', '\\', whitespace, or control chars; no leading '.' or '-'; max 80 chars)", t), nil)
		}
	}
	for _, c := range categories {
		if err := validateSlug(c); err != nil {
			return camperrors.NewValidation("category",
				fmt.Sprintf("invalid --category value %q: must be a path-safe workflow category (no '/', '\\', whitespace, or control chars; no leading '.' or '-'; max 80 chars)", c), nil)
		}
	}
	for _, s := range stages {
		if !wkitem.IsValidStageForTypes(wkitem.LifecycleStage(s), types) {
			return camperrors.NewValidation("stage",
				fmt.Sprintf("unknown --stage value: %q (valid stages depend on --type; built-in stages: none, inbox, active, ready, planning, ritual, chains)", s), nil)
		}
	}
	for _, s := range attentionStages {
		if !isValidAttentionStage(s) {
			return camperrors.NewValidation("attention_stage",
				fmt.Sprintf("unknown --attention-stage value: %q (valid: current, next, active, parked)", s), nil)
		}
	}
	for _, group := range groups {
		if !priority.ValidGroup(group) {
			return camperrors.NewValidation("group",
				fmt.Sprintf("invalid --group value %q: must be lowercase letters, numbers, dash, or underscore; no leading dash or dot; max 80 chars", group), nil)
		}
	}
	switch groupBy {
	case "", "attention_stage", "group", "type", "category":
	default:
		return camperrors.NewValidation("group_by",
			fmt.Sprintf("unknown --group-by value %q (valid: attention_stage, group, type, category)", groupBy), nil)
	}
	if jsonMode && printMode {
		return camperrors.NewValidation("output_mode", "--json and --print are mutually exclusive", nil)
	}
	if jsonMode && listMode {
		return camperrors.NewValidation("output_mode", "--json and --list are mutually exclusive", nil)
	}
	if listMode && printMode {
		return camperrors.NewValidation("output_mode", "--list and --print are mutually exclusive", nil)
	}
	if jsonMode && pathOutput != "" {
		return camperrors.NewValidation("output_mode", "--json and --path-output are mutually exclusive", nil)
	}
	if listMode && pathOutput != "" {
		return camperrors.NewValidation("output_mode", "--list and --path-output are mutually exclusive", nil)
	}
	if printMode && pathOutput != "" {
		return camperrors.NewValidation("output_mode", "--print and --path-output are mutually exclusive", nil)
	}
	return nil
}

func outputJSON(campaignRoot string, cfg *config.CampaignConfig, items []wkitem.WorkItem, groupBy string) error {
	payload := wkitem.NewPayloadWithGrouping(campaignRoot, items, groupBy)
	payload.CategoryVocabulary = categoryVocabulary(cfg)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func categoryVocabulary(cfg *config.CampaignConfig) []wkitem.CategoryVocabEntry {
	cats := cfg.WorkflowCategories()
	keys := cfg.OrderedWorkflowCategoryKeys()
	out := make([]wkitem.CategoryVocabEntry, 0, len(keys))
	for _, k := range keys {
		c := cats[k]
		out = append(out, wkitem.CategoryVocabEntry{Key: k, Label: c.Label, Description: c.Description})
	}
	return out
}

func runTUI(ctx context.Context, items []wkitem.WorkItem, printOnly bool, pathOutput string, campaignRoot string, resolver *paths.Resolver, store *priority.Store, storePath string, showParked bool, categoryForType func(string) string) error {
	if len(items) == 0 {
		return camperrors.Newf("no work items found")
	}

	model := wktui.New(ctx, items, campaignRoot, resolver, store, storePath, showParked)
	model.SetCategoryResolver(categoryForType)
	p := tea.NewProgram(model, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return camperrors.Wrap(err, "TUI error")
	}
	m, ok := result.(wktui.Model)
	if !ok || m.Selected == nil {
		return nil
	}
	return runSelectedAction(ctx, *m.Selected, printOnly, pathOutput, campaignRoot)
}
