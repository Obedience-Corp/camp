package selector

import (
	"strings"

	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// Filter keeps items matching query, then Order.
// Empty or whitespace query returns Order(items) unchanged — Score("", _) is 0
// and must not drop every row.
func Filter(items []Item, query string) []Item {
	q := strings.TrimSpace(query)
	if q == "" {
		return Order(items)
	}
	matched := make([]Item, 0, len(items))
	for _, it := range items {
		if itemMatches(it, q) {
			matched = append(matched, it)
		}
	}
	return Order(matched)
}

func itemMatches(it Item, query string) bool {
	labelScore, _ := navfuzzy.Score(query, it.Label)
	filterScore := 0
	if it.Filter != "" {
		filterScore, _ = navfuzzy.Score(query, it.Filter)
	}
	return max(labelScore, filterScore) > 0
}
