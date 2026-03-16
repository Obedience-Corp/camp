package workitem

import (
	"slices"
	"strings"
)

// Sort orders items by the design's deterministic rule:
//
//	primary:   sort_timestamp DESC (most recent first)
//	secondary: created_at DESC
//	tertiary:  relative_path ASC (alphabetical for stable tie-breaking)
func Sort(items []WorkItem) {
	slices.SortStableFunc(items, func(a, b WorkItem) int {
		if c := b.SortTimestamp.Compare(a.SortTimestamp); c != 0 {
			return c
		}
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
}
