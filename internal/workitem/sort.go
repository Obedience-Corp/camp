package workitem

import (
	"cmp"
	"slices"
	"strings"
)

// Sort orders items by the design's deterministic rule:
//
//	primary:   ManualPriority bucket (high=1 < medium=2 < low=3 < none=4)
//	secondary: sort_timestamp DESC (most recent first)
//	tertiary:  created_at DESC
//	quaternary: relative_path ASC (alphabetical for stable tie-breaking)
func Sort(items []WorkItem) {
	slices.SortStableFunc(items, func(a, b WorkItem) int {
		if c := cmp.Compare(priorityRank(a.ManualPriority), priorityRank(b.ManualPriority)); c != 0 {
			return c
		}
		if c := b.SortTimestamp.Compare(a.SortTimestamp); c != 0 {
			return c
		}
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
}

// priorityRank maps a manual priority string to its sort rank.
func priorityRank(p string) int {
	switch p {
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}
