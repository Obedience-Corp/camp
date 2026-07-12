package org

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type orgMove struct {
	Campaign string `json:"campaign"`
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type orgMoveResult struct {
	Org       string    `json:"org"`
	Moved     []orgMove `json:"moved"`
	Unchanged []string  `json:"unchanged"`
}

var orgAddCmd = &cobra.Command{
	Use:   "add <org> <campaign>...",
	Short: "Assign campaigns to an org (reassigns; single-membership)",
	Long: `Assign one or more campaigns to <org>.

Membership is single, so this is also the reassign verb: a campaign added to a
new org leaves its previous org in the same step. The org is created implicitly.
Adding a campaign already in <org> is a no-op for that campaign.`,
	Example: `  camp org add obey obey-campaign obey-content
  camp org add client-acme acme-site --json`,
	Args: cobra.MinimumNArgs(2),
	RunE: runOrgAdd,
}

var orgRemoveCmd = &cobra.Command{
	Use:     "remove <campaign>...",
	Aliases: []string{"rm"},
	Short:   "Return campaigns to the default org",
	Long: `Return one or more campaigns to the "default" org.

Since a campaign is always in exactly one org, you do not name the org.
Removing a campaign already in "default" is a no-op.`,
	Example: `  camp org remove obey-content
  camp org remove acme-site other-site --json`,
	Args: cobra.MinimumNArgs(1),
	RunE: runOrgRemove,
}

func init() {
	Cmd.AddCommand(orgAddCmd)
	Cmd.AddCommand(orgRemoveCmd)
	orgAddCmd.Flags().Bool("json", false, "Output as JSON")
	orgRemoveCmd.Flags().Bool("json", false, "Output as JSON")
}

func runOrgAdd(cmd *cobra.Command, args []string) error {
	org := args[0]
	if err := validateOrgName(org); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	return reassignOrg(cmd, func(*config.Registry) string { return org }, args[1:], asJSON)
}

func runOrgRemove(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	return reassignOrg(cmd, (*config.Registry).FallbackOrg, args, asJSON)
}

func validateOrgName(name string) error {
	return config.ValidateName("org", name)
}

func reassignOrg(cmd *cobra.Command, target func(*config.Registry) string, campaignArgs []string, asJSON bool) error {
	result := orgMoveResult{Moved: []orgMove{}, Unchanged: []string{}}

	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		targetOrg := target(reg)
		result.Org = targetOrg
		ensureOrg(reg, targetOrg)
		resolved, err := resolveUnique(reg, campaignArgs)
		if err != nil {
			return err
		}
		for _, c := range resolved {
			if c.Org == targetOrg {
				result.Unchanged = append(result.Unchanged, c.Name)
				continue
			}
			entry := reg.Campaigns[c.ID]
			entry.Org = targetOrg
			reg.Campaigns[c.ID] = entry
			result.Moved = append(result.Moved, orgMove{Campaign: c.Name, ID: c.ID, From: c.Org, To: targetOrg})
		}
		return nil
	})
	if err != nil {
		return err
	}
	return renderOrgMoveResult(cmd.OutOrStdout(), result, asJSON)
}

// ensureOrg appends name to reg.Orgs if it is not already present. Idempotent.
func ensureOrg(reg *config.Registry, name string) {
	reg.EnsureOrg(name)
}

func orgExists(reg *config.Registry, name string) bool {
	for _, o := range reg.Orgs {
		if o.Name == name {
			return true
		}
	}
	return false
}

func removeOrg(reg *config.Registry, name string) {
	out := make([]config.OrgEntry, 0, len(reg.Orgs))
	for _, o := range reg.Orgs {
		if o.Name == name {
			continue
		}
		out = append(out, o)
	}
	reg.Orgs = out
}

// renameOrgEntry renames an OrgEntry in reg.Orgs. No-op if oldName is absent.
func renameOrgEntry(reg *config.Registry, oldName, newName string) {
	for i, o := range reg.Orgs {
		if o.Name == oldName {
			reg.Orgs[i].Name = newName
			return
		}
	}
}

// membersOf returns campaigns whose Org equals name, ordered by name then ID so
// callers (e.g. --force delete reassignment) produce stable, deterministic output.
func membersOf(reg *config.Registry, name string) []config.RegisteredCampaign {
	var members []config.RegisteredCampaign
	for _, c := range reg.Campaigns {
		if c.Org == name {
			members = append(members, c)
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Name != members[j].Name {
			return members[i].Name < members[j].Name
		}
		return members[i].ID < members[j].ID
	})
	return members
}

func resolveUnique(reg *config.Registry, queries []string) ([]config.RegisteredCampaign, error) {
	seen := make(map[string]bool, len(queries))
	resolved := make([]config.RegisteredCampaign, 0, len(queries))
	for _, q := range queries {
		c, ok := reg.Get(q)
		if !ok {
			return nil, camperrors.NewNotFound("campaign", q, nil)
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		resolved = append(resolved, c)
	}
	return resolved, nil
}

func renderOrgMoveResult(w io.Writer, r orgMoveResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	_, err := io.WriteString(w, formatOrgMoveResult(r))
	return err
}

func formatOrgMoveResult(r orgMoveResult) string {
	var lines []string
	switch len(r.Moved) {
	case 0:
		lines = append(lines, fmt.Sprintf("no changes: all campaigns already in org %q", r.Org))
	case 1:
		m := r.Moved[0]
		lines = append(lines, fmt.Sprintf("moved %q to org %q (was %q)", m.Campaign, m.To, m.From))
	default:
		lines = append(lines, fmt.Sprintf("moved %d campaigns to org %q", len(r.Moved), r.Org))
		for _, m := range r.Moved {
			lines = append(lines, fmt.Sprintf("  %s (was %q)", m.Campaign, m.From))
		}
	}
	if len(r.Unchanged) > 0 {
		lines = append(lines, fmt.Sprintf("unchanged (already in %q): %s", r.Org, strings.Join(r.Unchanged, ", ")))
	}
	return strings.Join(lines, "\n") + "\n"
}
