package festivals

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
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
		if _, err := fmt.Fprintln(w, org); err != nil {
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
		if _, err := fmt.Fprintf(w, "  %s\n", name); err != nil {
			return err
		}
		if err := renderFestivalRows(w, byCampaign[name]); err != nil {
			return err
		}
	}
	return nil
}

func renderFestivalRows(w io.Writer, items []festivalItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, it := range items {
		row := fmt.Sprintf("    %s\t%s\t%d/%d", strings.ToUpper(it.Status), it.Festival, it.Progress.Completed, it.Progress.Total)
		if _, err := fmt.Fprintln(tw, row); err != nil {
			return err
		}
	}
	return tw.Flush()
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
