package festivals

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/charmbracelet/lipgloss"
)

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderFestivalsHuman(w io.Writer, items []festivalItem, fallbackOrg string) error {
	if len(items) == 0 {
		_, err := io.WriteString(w, "no festivals found\n")
		return err
	}

	byOrg := groupByOrgCampaign(items)
	orgs := sortedOrgKeys(byOrg, fallbackOrg)
	for i, org := range orgs {
		if i > 0 {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, festOrgHeader.Render(org)); err != nil {
			return err
		}
		if err := renderOrgSection(w, byOrg[org]); err != nil {
			return err
		}
	}
	return nil
}

func renderOrgSection(w io.Writer, byCampaign map[string][]festivalItem) error {
	campaigns := make([]string, 0, len(byCampaign))
	for name := range byCampaign {
		campaigns = append(campaigns, name)
	}
	sort.Strings(campaigns)

	for _, name := range campaigns {
		if _, err := fmt.Fprintf(w, "  %s\n", festCampaignHeader.Render(name)); err != nil {
			return err
		}
		if err := renderFestivalRows(w, byCampaign[name]); err != nil {
			return err
		}
	}
	return nil
}

func renderFestivalRows(w io.Writer, items []festivalItem) error {
	// Shared name width for this campaign, measured on the UNSTYLED names.
	nameW := 0
	for _, it := range items {
		if n := lipgloss.Width(it.Festival); n > nameW {
			nameW = n
		}
	}
	if nameW > festNameMax {
		nameW = festNameMax
	}
	if nameW < festNameMin {
		nameW = festNameMin
	}
	cw := nameW + 2 + festStatusW + 2 + festProgressW
	for _, it := range items {
		// four-space indent under the campaign header; festRow adds no gutter.
		if _, err := fmt.Fprintf(w, "    %s\n", festRow(it, cw, false)); err != nil {
			return err
		}
	}
	return nil
}

func groupByOrgCampaign(items []festivalItem) map[string]map[string][]festivalItem {
	byOrg := make(map[string]map[string][]festivalItem)
	for _, it := range items {
		if byOrg[it.Org] == nil {
			byOrg[it.Org] = make(map[string][]festivalItem)
		}
		byOrg[it.Org][it.Campaign] = append(byOrg[it.Org][it.Campaign], it)
	}
	return byOrg
}

func sortedOrgKeys(byOrg map[string]map[string][]festivalItem, fallbackOrg string) []string {
	orgs := make([]string, 0, len(byOrg))
	for org := range byOrg {
		orgs = append(orgs, org)
	}
	sort.Slice(orgs, func(i, j int) bool {
		if (orgs[i] == fallbackOrg) != (orgs[j] == fallbackOrg) {
			return orgs[i] == fallbackOrg
		}
		return orgs[i] < orgs[j]
	})
	return orgs
}
