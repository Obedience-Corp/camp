package main

import (
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

type listFilter struct {
	org    string
	tags   []string
	status string
	all    bool
}

func parseListFilter(cmd *cobra.Command) (listFilter, error) {
	org, _ := cmd.Flags().GetString("org")
	tags, _ := cmd.Flags().GetStringSlice("tag")
	status, _ := cmd.Flags().GetString("status")
	all, _ := cmd.Flags().GetBool("all")
	if status != "" {
		if err := config.ValidateStatus(status); err != nil {
			return listFilter{}, err
		}
	}
	return listFilter{org: org, tags: tags, status: status, all: all}, nil
}

func filterEntries(entries []campaignEntry, f listFilter) []campaignEntry {
	out := make([]campaignEntry, 0, len(entries))
	for _, e := range entries {
		if !statusMatches(e, f) {
			continue
		}
		if f.org != "" && e.Org != f.org {
			continue
		}
		if !hasAllTags(e, f.tags) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func statusMatches(e campaignEntry, f listFilter) bool {
	if f.status != "" {
		return e.Status == f.status
	}
	if f.all {
		return true
	}
	return e.Status == config.StatusActive
}

func hasAllTags(e campaignEntry, tags []string) bool {
	for _, want := range tags {
		found := false
		for _, t := range e.Tags {
			if t == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func shouldGroup(cmd *cobra.Command, entries []campaignEntry) bool {
	noGroup, _ := cmd.Flags().GetBool("no-group")
	if noGroup {
		return false
	}
	group, _ := cmd.Flags().GetBool("group")
	if group {
		return true
	}
	return distinctOrgs(entries) > 1
}

func distinctOrgs(entries []campaignEntry) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Org] = true
	}
	return len(seen)
}
