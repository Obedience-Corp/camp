package org

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// stdoutIsTTY reports whether stdout is an interactive terminal. It is a
// package variable so tests can make the bare-org dispatch deterministic.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type orgRenameResult struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	Reassigned int    `json:"reassigned"`
}

type orgCount struct {
	Org       string `json:"org"`
	Campaigns int    `json:"campaigns"`
	Active    int    `json:"active"`
}

type orgMember struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Tags   []string `json:"tags"`
}

type orgShowResult struct {
	Org       string      `json:"org"`
	Campaigns int         `json:"campaigns"`
	Active    int         `json:"active"`
	Members   []orgMember `json:"members"`
}

var orgRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename an org, reassigning all members atomically",
	Long: `Rename <old> to <new>, reassigning every member in one atomic write.

Errors if <old> has no members or if <new> already exists (no implicit merge).
Renaming the fallback org ("default" by default) makes <new> the new fallback.`,
	Example: `  camp org rename obey obedience`,
	Args:    cobra.ExactArgs(2),
	RunE:    runOrgRename,
}

var orgListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List orgs with member and active counts",
	Example: `  camp org list`,
	Args:    cobra.NoArgs,
	RunE:    runOrgList,
}

var orgShowCmd = &cobra.Command{
	Use:     "show <org>",
	Short:   "Show an org's member campaigns",
	Example: `  camp org show obey`,
	Args:    cobra.ExactArgs(1),
	RunE:    runOrgShow,
}

var orgWhichCmd = &cobra.Command{
	Use:     "which",
	Aliases: []string{"current"},
	Short:   "Print the current campaign's org",
	Example: `  camp org which`,
	Args:    cobra.NoArgs,
	RunE:    runOrgWhich,
}

func init() {
	Cmd.AddCommand(orgRenameCmd)
	Cmd.AddCommand(orgListCmd)
	Cmd.AddCommand(orgShowCmd)
	Cmd.AddCommand(orgWhichCmd)
	orgRenameCmd.Flags().Bool("json", false, "Output as JSON")
	orgListCmd.Flags().Bool("json", false, "Output as JSON")
	orgShowCmd.Flags().Bool("json", false, "Output as JSON")
	orgWhichCmd.Flags().Bool("json", false, "Output as JSON")
	Cmd.Flags().Bool("json", false, "Output as JSON")
	Cmd.Flags().BoolP("interactive", "i", false, "Open the interactive org browser (prints the org list when stdout is not a terminal)")
}

func runOrgRename(cmd *cobra.Command, args []string) error {
	oldOrg, newOrg := args[0], args[1]
	if err := validateOrgName(newOrg); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	result := orgRenameResult{Old: oldOrg, New: newOrg}
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		n, err := renameOrgInRegistry(reg, oldOrg, newOrg)
		if err != nil {
			return err
		}
		result.Reassigned = n
		return nil
	})
	if err != nil {
		return err
	}
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), result)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "renamed org %q -> %q (%d campaigns reassigned)\n",
		result.Old, result.New, result.Reassigned)
	return err
}

func renameOrgInRegistry(reg *config.Registry, oldOrg, newOrg string) (int, error) {
	if oldOrg == newOrg {
		return 0, camperrors.NewValidation("org", "old and new org are the same: \""+oldOrg+"\"", nil)
	}
	fallback := reg.FallbackOrg()
	isFallback := oldOrg == fallback

	var memberIDs []string
	for _, c := range reg.ListAll() {
		switch c.Org {
		case newOrg:
			if newOrg != oldOrg {
				return 0, camperrors.NewValidation("org",
					"org \""+newOrg+"\" already exists; no implicit merge", nil)
			}
		case oldOrg:
			memberIDs = append(memberIDs, c.ID)
		}
	}
	if len(memberIDs) == 0 && !isFallback {
		return 0, camperrors.NewNotFound("org", oldOrg, nil)
	}
	for _, id := range memberIDs {
		e := reg.Campaigns[id]
		e.Org = newOrg
		reg.Campaigns[id] = e
	}
	if isFallback {
		if newOrg == config.DefaultOrg {
			reg.DefaultOrg = ""
		} else {
			reg.DefaultOrg = newOrg
		}
	}
	return len(memberIDs), nil
}

func runOrgList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	reg, err := config.LoadRegistry(cmd.Context())
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	counts := computeOrgCounts(reg)
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), counts)
	}
	return writeOrgCounts(cmd.OutOrStdout(), counts)
}

func computeOrgCounts(reg *config.Registry) []orgCount {
	byOrg := make(map[string]*orgCount)
	// Seed every persisted first-class org at zero so empty orgs appear in list.
	for _, o := range reg.Orgs {
		byOrg[o.Name] = &orgCount{Org: o.Name}
	}
	for _, c := range reg.ListAll() {
		oc := byOrg[c.Org]
		if oc == nil {
			// Defensive: reconcileOrgs should prevent missing membership orgs.
			oc = &orgCount{Org: c.Org}
			byOrg[c.Org] = oc
		}
		oc.Campaigns++
		if c.Status == config.StatusActive {
			oc.Active++
		}
	}
	fallback := reg.FallbackOrg()
	out := make([]orgCount, 0, len(byOrg))
	for _, oc := range byOrg {
		out = append(out, *oc)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Org == fallback) != (out[j].Org == fallback) {
			return out[i].Org == fallback
		}
		return out[i].Org < out[j].Org
	})
	return out
}

func writeOrgCounts(w io.Writer, counts []orgCount) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", ui.Label("ORG"), ui.Label("CAMPAIGNS"), ui.Label("ACTIVE")); err != nil {
		return err
	}
	for _, c := range counts {
		active := ui.Dim(fmt.Sprintf("%d", c.Active))
		if c.Active > 0 {
			active = ui.Success(fmt.Sprintf("%d", c.Active))
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", ui.Accent(c.Org), ui.Value(fmt.Sprintf("%d", c.Campaigns)), active); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// styleOrgStatus renders a campaign lifecycle status with a semantic color.
func styleOrgStatus(status string) string {
	switch status {
	case config.StatusActive:
		return ui.Success(status)
	case config.StatusInactive:
		return ui.Warning(status)
	case config.StatusReference:
		return ui.Info(status)
	default:
		return ui.Dim(status)
	}
}

func runOrgShow(cmd *cobra.Command, args []string) error {
	org := args[0]
	asJSON, _ := cmd.Flags().GetBool("json")
	reg, err := config.LoadRegistry(cmd.Context())
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	if !orgExists(reg, org) {
		return camperrors.NewNotFound("org", org, nil)
	}
	result := buildOrgShow(reg, org)
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), result)
	}
	return writeOrgShow(cmd.OutOrStdout(), result)
}

func buildOrgShow(reg *config.Registry, org string) orgShowResult {
	result := orgShowResult{Org: org, Members: []orgMember{}}
	for _, c := range reg.ListAll() {
		if c.Org != org {
			continue
		}
		result.Campaigns++
		if c.Status == config.StatusActive {
			result.Active++
		}
		result.Members = append(result.Members, orgMember{ID: c.ID, Name: c.Name, Status: c.Status, Tags: c.Tags})
	}
	sort.Slice(result.Members, func(i, j int) bool {
		return result.Members[i].Name < result.Members[j].Name
	})
	return result
}

func writeOrgShow(w io.Writer, r orgShowResult) error {
	if _, err := fmt.Fprintf(w, "%s %s   %s\n\n",
		ui.Label("org:"), ui.Accent(r.Org),
		ui.Dim(fmt.Sprintf("(%d campaigns, %d active)", r.Campaigns, r.Active))); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", ui.Label("ID"), ui.Label("NAME"), ui.Label("STATUS"), ui.Label("TAGS")); err != nil {
		return err
	}
	for _, m := range r.Members {
		tags := ui.Dim("-")
		if len(m.Tags) > 0 {
			tags = ui.Info(strings.Join(m.Tags, ","))
		}
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", ui.Dim(m.ID), ui.Value(m.Name), styleOrgStatus(m.Status), tags); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// runOrgBare is the default action for `camp org`. In an interactive terminal it
// opens the org browser; otherwise (piped, or --json) it prints the current
// campaign's org. Use `camp org which` to print the org unconditionally.
func runOrgBare(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	interactive, _ := cmd.Flags().GetBool("interactive")
	if shouldOpenOrgTUI(asJSON, interactive, stdoutIsTTY()) {
		return runOrgTUI(cmd)
	}
	return printCurrentOrg(cmd, asJSON)
}

// shouldOpenOrgTUI decides whether bare `camp org` opens the interactive
// browser. --json always means machine output; otherwise an explicit -i or an
// interactive terminal opens the TUI.
func shouldOpenOrgTUI(asJSON, interactive, isTTY bool) bool {
	if asJSON {
		return false
	}
	return interactive || isTTY
}

func runOrgWhich(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	return printCurrentOrg(cmd, asJSON)
}

func printCurrentOrg(cmd *cobra.Command, asJSON bool) error {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return err
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}

	org := reg.FallbackOrg()
	name := cfg.Name
	if c, ok := reg.GetByID(cfg.ID); ok {
		org = c.Org
		name = c.Name
	}
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), map[string]string{"campaign": name, "org": org})
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), org)
	return err
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
