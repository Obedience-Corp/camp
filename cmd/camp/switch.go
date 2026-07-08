package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
)

var switchCmd = &cobra.Command{
	Use:   "switch [campaign]",
	Short: "Switch to a different campaign",
	Long: `Switch to a registered campaign by name or ID.

Without arguments, opens an interactive picker to select a campaign.
With an argument, looks up the campaign by name or ID prefix.
Use --org or org/campaign to resolve inside one organization.

Use with the cgo shell function for instant navigation:
  cgo switch                 # Interactive campaign picker
  cgo switch my-campaign     # Switch by name
  cgo switch a1b2             # Switch by ID prefix
  cgo switch obey/platform    # Switch by org-scoped selector

The --print flag outputs just the path for shell integration:
  cd "$(camp switch --print)"

Use campaign@tab to navigate to a specific location in the target campaign:
  camp switch obey-campaign@p    # Switch and navigate to projects/
  camp switch obey/platform@f    # Switch inside org and navigate to festivals/`,
	Example: `  camp switch                        # Interactive picker
  camp switch obey-campaign          # Switch by name
  camp switch --org obey platform    # Switch by name within an org
  camp switch obey/platform          # Switch by scoped selector
  camp switch a1b2                   # Switch by ID prefix
  camp switch --print                # Picker, output path only
  camp switch obey-campaign@p        # Switch and navigate to projects/
  camp switch --all old-reference    # Include inactive/reference campaigns
  camp switch --org obey platform --json`,
	Aliases: []string{"sw"},
	Args:    cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use: camp switch <name> --print",
		"interactive":   "true",
	},
	RunE: runSwitch,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx := cmd.Context()
		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		scope, err := switchScopeFromFlags(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		if at := strings.IndexByte(toComplete, '@'); at >= 0 {
			campaignQuery := toComplete[:at]
			tabPrefix := toComplete[at+1:]
			tabs := completeSwitchTabs(ctx, reg, campaignQuery, tabPrefix, scope)
			completions := make([]string, len(tabs))
			for i, t := range tabs {
				completions[i] = campaignQuery + "@" + t
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
		}

		return completeSwitchCampaigns(reg, scope, toComplete), cobra.ShellCompDirectiveNoFileComp
	},
}

const switchSchemaVersion = "camp-switch/v1"

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.GroupID = "global"
	switchCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
	switchCmd.Flags().String("org", "", "Only switch among campaigns in this org")
	switchCmd.Flags().String("status", "", "Only switch among campaigns with this lifecycle status")
	switchCmd.Flags().Bool("all", false, "Include inactive and reference campaigns")
	switchCmd.Flags().Bool("json", false, "Output selected campaign and target path as JSON")
	_ = switchCmd.RegisterFlagCompletionFunc("org", completeSwitchOrgFlag)
	_ = switchCmd.RegisterFlagCompletionFunc("status", completeSwitchStatusFlag)
}

func resolveTabInCampaign(ctx context.Context, c config.RegisteredCampaign, tabKey string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, c.Path)
	if err != nil {
		return "", camperrors.Wrapf(err, "loading campaign config for %s", c.Name)
	}
	resolved := nav.ResolveConfiguredTarget(cfg, []string{tabKey})
	if !resolved.Matched {
		return "", camperrors.New(fmt.Sprintf("tab %q not found in campaign %s", tabKey, c.Name))
	}
	relativePath := resolved.RelativePath
	if relativePath == "" && resolved.Category != nav.CategoryAll {
		relativePath = resolved.Category.Dir()
	}
	if relativePath == "" {
		return "", camperrors.New(fmt.Sprintf("tab %q resolved to campaign root in %s", tabKey, c.Name))
	}
	return filepath.Join(c.Path, relativePath), nil
}

func completeSwitchTabs(ctx context.Context, reg *config.Registry, campaignQuery, tabPrefix string, scope cmdutil.CampaignScope) []string {
	parsed := cmdutil.ParseSwitchSelector(campaignQuery)
	if parsed.Org != "" {
		if scope.Org != "" && scope.Org != parsed.Org {
			return nil
		}
		scope.Org = parsed.Org
	}
	c, err := cmdutil.ResolveCampaignSelectionScoped(parsed.Campaign, reg, scope, nil)
	if err != nil {
		return nil
	}

	cfg, err := config.LoadCampaignConfig(ctx, c.Path)
	if err != nil {
		return nil
	}

	all := nav.TopLevelNavigationNames(cfg)
	if tabPrefix == "" {
		return all
	}

	var filtered []string
	for _, name := range all {
		if strings.HasPrefix(name, tabPrefix) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func completeSwitchCampaigns(reg *config.Registry, scope cmdutil.CampaignScope, toComplete string) []string {
	if slash := strings.IndexByte(toComplete, '/'); slash >= 0 {
		orgPart := toComplete[:slash]
		campaignPrefix := toComplete[slash+1:]
		orgs := switchOrgs(reg)
		if !hasString(orgs, orgPart) {
			return filterStrings(withSlash(orgs), orgPart)
		}
		if scope.Org != "" && scope.Org != orgPart {
			return nil
		}
		scope.Org = orgPart
		return prefixedCampaignCompletions(reg, scope, orgPart+"/", campaignPrefix)
	}

	if scope.Org != "" {
		return prefixedCampaignCompletions(reg, scope, "", toComplete)
	}

	var candidates []string
	candidates = append(candidates, withSlash(switchOrgs(reg))...)
	for _, c := range cmdutil.FilterCampaigns(reg, scope) {
		candidates = append(candidates, c.Name)
	}
	return filterStrings(candidates, toComplete)
}

func prefixedCampaignCompletions(reg *config.Registry, scope cmdutil.CampaignScope, prefix, campaignPrefix string) []string {
	var candidates []string
	for _, c := range cmdutil.FilterCampaigns(reg, scope) {
		candidates = append(candidates, prefix+c.Name)
	}
	return filterStrings(candidates, prefix+campaignPrefix)
}

func filterStrings(candidates []string, query string) []string {
	sort.Strings(candidates)
	if query == "" {
		return candidates
	}
	matches := navfuzzy.Filter(candidates, query)
	return matches.Targets()
}

func switchOrgs(reg *config.Registry) []string {
	seen := map[string]struct{}{}
	for _, c := range reg.ListAll() {
		if c.Org == "" {
			c.Org = reg.FallbackOrg()
		}
		seen[c.Org] = struct{}{}
	}
	orgs := make([]string, 0, len(seen))
	for org := range seen {
		orgs = append(orgs, org)
	}
	sort.Strings(orgs)
	return orgs
}

func withSlash(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v + "/"
	}
	return out
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func completeSwitchOrgFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	reg, err := config.LoadRegistry(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return filterStrings(switchOrgs(reg), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeSwitchStatusFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterStrings(config.ValidStatuses(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func runSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")
	jsonOut, _ := cmd.Flags().GetBool("json")
	if printOnly && jsonOut {
		return camperrors.New("cannot use --json with --print")
	}
	scope, err := switchScopeFromFlags(cmd)
	if err != nil {
		return err
	}

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "load registry")
	}
	if reg.Len() == 0 {
		return camperrors.Newf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign
	targetPath := ""
	targetTab := ""

	if len(args) == 1 {
		parsed, err := parseSwitchArg(args[0], scope)
		if err != nil {
			return err
		}
		if parsed.Org != "" {
			scope.Org = parsed.Org
		}
		c, err := cmdutil.ResolveCampaignSelectionScoped(parsed.Campaign, reg, scope, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		selected = c
		if parsed.HasTab {
			targetTab = parsed.Tab
			targetPath, err = resolveTabInCampaign(ctx, c, parsed.Tab)
			if err != nil {
				return err
			}
		}
	} else {
		if !tui.IsTerminal() {
			return camperrors.New("campaign name required in non-interactive mode (use 'camp switch <name>' or run interactively)")
		}
		c, err := cmdutil.PickCampaignWithOptions(ctx, reg, cmdutil.PickCampaignOptions{Scope: scope})
		if err != nil {
			return err
		}
		selected = c
	}
	if targetPath == "" {
		targetPath = selected.Path
	}

	if err := config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		reg.UpdateLastAccess(selected.ID)
		return nil
	}); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: failed to update last access: %v\n", err)
	}

	return emitSwitchSelection(cmd, selected, targetPath, targetTab, printOnly, jsonOut)
}

func switchScopeFromFlags(cmd *cobra.Command) (cmdutil.CampaignScope, error) {
	org, _ := cmd.Flags().GetString("org")
	status, _ := cmd.Flags().GetString("status")
	all, _ := cmd.Flags().GetBool("all")
	if org != "" {
		if err := config.ValidateName("org", org); err != nil {
			return cmdutil.CampaignScope{}, err
		}
	}
	if status != "" {
		if err := config.ValidateStatus(status); err != nil {
			return cmdutil.CampaignScope{}, err
		}
	}
	if status != "" && all {
		return cmdutil.CampaignScope{}, camperrors.New("cannot use --status with --all")
	}
	return cmdutil.CampaignScope{Org: org, Status: status, All: all}, nil
}

func parseSwitchArg(raw string, scope cmdutil.CampaignScope) (cmdutil.ParsedSwitchSelector, error) {
	parsed := cmdutil.ParseSwitchSelector(raw)
	if parsed.Org != "" {
		if err := config.ValidateName("org", parsed.Org); err != nil {
			return parsed, err
		}
		if strings.Contains(parsed.Campaign, "/") {
			return parsed, camperrors.New("switch selector may contain at most one org separator")
		}
		if parsed.Campaign == "" {
			return parsed, camperrors.New("campaign name required after org selector")
		}
		if scope.Org != "" && scope.Org != parsed.Org {
			return parsed, camperrors.New(fmt.Sprintf("selector org %q conflicts with --org %q", parsed.Org, scope.Org))
		}
	}
	return parsed, nil
}

type switchOutput struct {
	SchemaVersion string               `json:"schema_version"`
	Campaign      switchCampaignOutput `json:"campaign"`
	Target        switchTargetOutput   `json:"target"`
}

type switchCampaignOutput struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Org    string `json:"org"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

type switchTargetOutput struct {
	Tab  string `json:"tab,omitempty"`
	Path string `json:"path"`
}

func emitSwitchSelection(cmd *cobra.Command, selected config.RegisteredCampaign, targetPath, targetTab string, printOnly, jsonOut bool) error {
	if printOnly {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), targetPath)
		return err
	}
	if jsonOut {
		out := switchOutput{
			SchemaVersion: switchSchemaVersion,
			Campaign: switchCampaignOutput{
				ID:     selected.ID,
				Name:   selected.Name,
				Org:    selected.Org,
				Status: selected.Status,
				Path:   selected.Path,
			},
			Target: switchTargetOutput{
				Tab:  targetTab,
				Path: targetPath,
			},
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "cd %s\n", targetPath)
	return err
}
