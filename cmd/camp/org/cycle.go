package org

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/spf13/cobra"
)

const cycleSchemaVersion = "camp-org-cycle/v1"

var orgNextCmd = &cobra.Command{
	Use:     "next",
	Aliases: []string{"cycle"},
	Short:   "Switch to the next campaign in the current campaign's org",
	Long: `Switch to the next campaign in the current campaign's org.

Members are ordered by name, so the cycle is stable and predictable
(a -> b -> c -> a). By default only active campaigns are cycled; use --all to
include inactive and reference campaigns.

Use with the corg shell function for instant navigation:
  corg        # cd to the next campaign in this org

The --print flag outputs just the target path for shell integration, and --json
emits the resolved source and target campaigns.`,
	Example: `  camp org next            # Print cd to the next org campaign
  camp org next --print    # Print the target path only
  camp org next --all      # Include inactive/reference campaigns
  camp org next --json`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive; --print/--json resolve the next org campaign without a TUI",
	},
	RunE: runOrgNext,
}

var orgToggleCmd = &cobra.Command{
	Use:     "toggle",
	Aliases: []string{"back", "t"},
	Short:   "Toggle back to the last-visited campaign in the current org",
	Long: `Toggle back to the most recently visited other campaign in the current org.

"Most recently visited" is tracked by last-access time, which camp updates on
every 'camp switch' and 'camp org next'/'toggle'. Paired with 'camp org next',
this gives a natural A <-> B toggle within an org. By default only active
campaigns are considered; use --all to include inactive and reference campaigns.

Use with the corg shell function for instant navigation:
  corg t      # cd back to the last org campaign you were in`,
	Example: `  camp org toggle          # Print cd to the last-visited org campaign
  camp org toggle --print  # Print the target path only
  camp org toggle --json`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive; --print/--json resolve the last-visited org campaign without a TUI",
	},
	RunE: runOrgToggle,
}

func init() {
	Cmd.AddCommand(orgNextCmd)
	Cmd.AddCommand(orgToggleCmd)
	for _, c := range []*cobra.Command{orgNextCmd, orgToggleCmd} {
		c.Flags().Bool("print", false, "Print the target path only (for shell integration)")
		c.Flags().Bool("json", false, "Output the resolved source and target campaigns as JSON")
		c.Flags().Bool("all", false, "Include inactive and reference campaigns in the cycle")
		c.Flags().Bool("shell-connect", false, "Emit a shell line for the corg wrapper to eval (internal)")
		_ = c.Flags().MarkHidden("shell-connect")
	}
}

// cycleFlags holds the output-mode flags shared by next and toggle.
type cycleFlags struct {
	print        bool
	json         bool
	all          bool
	shellConnect bool
}

func cycleFlagsFrom(cmd *cobra.Command) (cycleFlags, error) {
	f := cycleFlags{}
	f.print, _ = cmd.Flags().GetBool("print")
	f.json, _ = cmd.Flags().GetBool("json")
	f.all, _ = cmd.Flags().GetBool("all")
	f.shellConnect, _ = cmd.Flags().GetBool("shell-connect")
	if f.print && f.json {
		return f, camperrors.New("cannot use --json with --print")
	}
	if f.shellConnect && (f.print || f.json) {
		return f, camperrors.New("--shell-connect cannot be combined with --print or --json")
	}
	return f, nil
}

func runOrgNext(cmd *cobra.Command, _ []string) error {
	return runCycle(cmd, "next", nextInOrgCycle)
}

func runOrgToggle(cmd *cobra.Command, _ []string) error {
	return runCycle(cmd, "toggle", mostRecentOther)
}

// cycleResolver picks the target campaign from the ordered org members given the
// current campaign's ID. ok is false when there is no distinct target.
type cycleResolver func(members []config.RegisteredCampaign, currentID string) (config.RegisteredCampaign, bool)

func runCycle(cmd *cobra.Command, action string, resolve cycleResolver) error {
	ctx := cmd.Context()
	flags, err := cycleFlagsFrom(cmd)
	if err != nil {
		return err
	}

	current, reg, err := currentCampaign(ctx)
	if err != nil {
		return err
	}

	scope := cmdutil.CampaignScope{Org: current.Org, All: flags.all}
	members := orderedCycleMembers(reg, scope)

	target, ok := resolve(members, current.ID)
	if !ok {
		// No distinct target: emit nothing on stdout so the corg wrapper's
		// `eval "$line"` is a no-op, and explain on stderr.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: %s\n", noTargetReason(action, current.Org))
		return nil
	}

	// Record the visit so a later toggle can find its way back, mirroring how
	// 'camp switch' maintains last-access ordering.
	if err := config.UpdateRegistry(ctx, func(r *config.Registry) error {
		r.UpdateLastAccess(target.ID)
		return nil
	}); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: failed to update last access: %v\n", err)
	}

	return emitCycleTarget(cmd.OutOrStdout(), action, current, target, flags)
}

// orderedCycleMembers returns the scoped org members ordered by name then ID so
// the forward cycle is stable regardless of registry map iteration order.
func orderedCycleMembers(reg *config.Registry, scope cmdutil.CampaignScope) []config.RegisteredCampaign {
	members := cmdutil.FilterCampaigns(reg, scope)
	sort.Slice(members, func(i, j int) bool {
		if members[i].Name != members[j].Name {
			return members[i].Name < members[j].Name
		}
		return members[i].ID < members[j].ID
	})
	return members
}

// nextInOrgCycle returns the member after currentID in the ordered ring. When
// the current campaign is not in the set (e.g. it is inactive and --all was not
// given), the first member is treated as "next". Returns false only when there
// is no member other than the current campaign.
func nextInOrgCycle(members []config.RegisteredCampaign, currentID string) (config.RegisteredCampaign, bool) {
	if len(members) == 0 {
		return config.RegisteredCampaign{}, false
	}
	idx := indexOfID(members, currentID)
	if idx < 0 {
		return members[0], members[0].ID != currentID
	}
	next := members[(idx+1)%len(members)]
	return next, next.ID != currentID
}

// mostRecentOther returns the member with the newest LastAccess other than the
// current campaign, ties broken by name then ID. Returns false when the org has
// no other member.
func mostRecentOther(members []config.RegisteredCampaign, currentID string) (config.RegisteredCampaign, bool) {
	var best config.RegisteredCampaign
	found := false
	for _, c := range members {
		if c.ID == currentID {
			continue
		}
		if !found || moreRecent(c, best) {
			best = c
			found = true
		}
	}
	return best, found
}

// moreRecent reports whether a should rank ahead of b for toggle: newer
// last-access first, then name, then ID for a deterministic tiebreak.
func moreRecent(a, b config.RegisteredCampaign) bool {
	if !a.LastAccess.Equal(b.LastAccess) {
		return a.LastAccess.After(b.LastAccess)
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

func indexOfID(members []config.RegisteredCampaign, id string) int {
	for i, c := range members {
		if c.ID == id {
			return i
		}
	}
	return -1
}

func noTargetReason(action, org string) string {
	switch action {
	case "toggle":
		return fmt.Sprintf("no other campaign in org %q to toggle to", org)
	default:
		return fmt.Sprintf("no other campaign in org %q to cycle to", org)
	}
}

// currentCampaign resolves the campaign containing the working directory along
// with the loaded registry entry, so callers get both the org membership and
// the shared registry for candidate resolution.
func currentCampaign(ctx context.Context) (config.RegisteredCampaign, *config.Registry, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return config.RegisteredCampaign{}, nil, camperrors.NewValidation("campaign",
			"not inside a campaign; run 'corg' from within a campaign in the org", err)
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return config.RegisteredCampaign{}, nil, err
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return config.RegisteredCampaign{}, nil, camperrors.Wrap(err, "failed to load registry")
	}
	c, ok := reg.GetByID(cfg.ID)
	if !ok {
		return config.RegisteredCampaign{}, nil, camperrors.NewValidation("campaign",
			"current campaign is not registered; run 'camp register' first", nil)
	}
	return c, reg, nil
}

type cycleCampaignOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Org  string `json:"org"`
	Path string `json:"path"`
}

type cycleOutput struct {
	SchemaVersion string              `json:"schema_version"`
	Action        string              `json:"action"`
	From          cycleCampaignOutput `json:"from"`
	To            cycleCampaignOutput `json:"to"`
}

func emitCycleTarget(w io.Writer, action string, from, to config.RegisteredCampaign, flags cycleFlags) error {
	switch {
	case flags.shellConnect:
		_, err := fmt.Fprintf(w, "cd -- %s\n", remote.ShellQuote(to.Path))
		return err
	case flags.print:
		_, err := fmt.Fprintln(w, to.Path)
		return err
	case flags.json:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(cycleOutput{
			SchemaVersion: cycleSchemaVersion,
			Action:        action,
			From:          cycleCampaignOutput{ID: from.ID, Name: from.Name, Org: from.Org, Path: from.Path},
			To:            cycleCampaignOutput{ID: to.ID, Name: to.Name, Org: to.Org, Path: to.Path},
		})
	default:
		_, err := fmt.Fprintf(w, "cd %s\n", to.Path)
		return err
	}
}
