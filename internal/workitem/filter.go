package workitem

import "strings"

// Filter applies type, stage, and query filters to a work item list.
// Filters are applied before any limit. Order is preserved.
func Filter(items []WorkItem, types, stages []string, query string) []WorkItem {
	if len(types) == 0 && len(stages) == 0 && query == "" {
		return items
	}

	typeSet := toSet(types)
	stageSet := toSet(stages)
	query = strings.ToLower(query)

	var result []WorkItem
	for _, item := range items {
		if len(typeSet) > 0 && !typeSet[string(item.WorkflowType)] {
			continue
		}
		if len(stageSet) > 0 && !stageSet[item.LifecycleStage] {
			continue
		}
		if query != "" && !matchesQuery(item, query) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func toSet(vals []string) map[string]bool {
	if len(vals) == 0 {
		return nil
	}
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

func matchesQuery(item WorkItem, query string) bool {
	return strings.Contains(strings.ToLower(item.Title), query) ||
		strings.Contains(strings.ToLower(item.RelativePath), query) ||
		strings.Contains(strings.ToLower(item.Summary), query)
}
