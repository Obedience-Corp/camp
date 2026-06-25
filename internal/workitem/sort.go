package workitem

import (
	"cmp"
	"slices"
	"strings"
)

// Sort orders items by the design's deterministic rule:
//
//	primary:   attention bucket (current < next < active < parked < none)
//	secondary: ManualPriority bucket (high < medium < low < none)
//	tertiary:  sort_timestamp DESC (most recent first)
//	next:      created_at DESC
//	final:     relative_path ASC (alphabetical for stable tie-breaking)
func Sort(items []WorkItem) {
	slices.SortStableFunc(items, func(a, b WorkItem) int {
		if c := cmp.Compare(attentionRank(a.AttentionStage), attentionRank(b.AttentionStage)); c != 0 {
			return c
		}
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

func attentionRank(stage string) int {
	switch stage {
	case "current":
		return 1
	case "next":
		return 2
	case "active":
		return 3
	case "parked":
		return 4
	default:
		return 5
	}
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
