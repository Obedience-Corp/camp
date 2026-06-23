package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func campaignTableCells(c campaignEntry) (id, name, org, typ, path string) {
	campaignType := c.Type
	if campaignType == "" {
		campaignType = "-"
	}
	shortID := c.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	orgCell := c.Org
	if orgCell == "" {
		orgCell = "-"
	}
	typeStyle := ui.GetCampaignTypeStyle(c.Type)
	return ui.Dim(shortID), ui.Value(c.Name), ui.Accent(orgCell), typeStyle.Render(campaignType), ui.Dim(c.Path)
}

func outputGrouped(entries []campaignEntry, format, fallbackOrg string) error {
	byOrg := make(map[string][]campaignEntry)
	for _, e := range entries {
		byOrg[e.Org] = append(byOrg[e.Org], e)
	}
	for i, org := range sortedGroupOrgs(byOrg, fallbackOrg) {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s %s\n", ui.Accent(org), ui.Dim(fmt.Sprintf("(%s)", ui.CountLabel(len(byOrg[org]), "campaign", "campaigns"))))
		if err := writeGroupSection(byOrg[org], format); err != nil {
			return err
		}
	}
	fmt.Println()
	fmt.Println(ui.Dim(ui.CountLabel(len(entries), "campaign", "campaigns")))
	return nil
}

func sortedGroupOrgs(byOrg map[string][]campaignEntry, fallbackOrg string) []string {
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

func writeGroupSection(entries []campaignEntry, format string) error {
	if format == "simple" {
		for _, c := range entries {
			if _, err := fmt.Printf("  %s\n", c.Name); err != nil {
				return err
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n",
		ui.Label("ID"), ui.Label("NAME"), ui.Label("TYPE"), ui.Label("PATH")); err != nil {
		return err
	}
	for _, c := range entries {
		id, name, _, typ, path := campaignTableCells(c)
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", id, name, typ, path); err != nil {
			return err
		}
	}
	return tw.Flush()
}
