package workitem

import "strings"

// Filter applies type, lifecycle stage, and query filters to a work item list.
// Filters are applied before any limit. Order is preserved.
func Filter(items []WorkItem, types, stages []string, query string) []WorkItem {
	return FilterAdvanced(items, FilterOptions{Types: types, LifecycleStages: stages, Query: query, ShowParked: true})
}

type FilterOptions struct {
	Types           []string
	Categories      []string
	Statuses        []string
	LifecycleStages []string
	AttentionStages []string
	Groups          []string
	Tags            []string
	Projects        []string
	Query           string
	ShowParked      bool
}

func FilterAdvanced(items []WorkItem, opts FilterOptions) []WorkItem {
	if len(opts.Types) == 0 && len(opts.Categories) == 0 && len(opts.Statuses) == 0 && len(opts.LifecycleStages) == 0 && len(opts.AttentionStages) == 0 && len(opts.Groups) == 0 && len(opts.Tags) == 0 && len(opts.Projects) == 0 && opts.Query == "" && opts.ShowParked {
		return items
	}

	typeSet := toSet(opts.Types)
	categorySet := toSet(opts.Categories)
	statusSet := normalizedStatusSet(opts.Statuses)
	stageSet := toSet(opts.LifecycleStages)
	attentionSet := toSet(opts.AttentionStages)
	groupSet := toSet(opts.Groups)
	projectSet := toSet(opts.Projects)
	query := strings.ToLower(opts.Query)

	var result []WorkItem
	for _, item := range items {
		if len(typeSet) > 0 && !typeSet[string(item.WorkflowType)] {
			continue
		}
		category := item.WorkflowCategory
		if category == "" {
			category = "uncategorized"
		}
		if len(categorySet) > 0 && !categorySet[category] {
			continue
		}
		if len(statusSet) > 0 && !statusSet[DisplayStatus(item)] {
			continue
		}
		if len(stageSet) > 0 && !stageSet[string(item.LifecycleStage)] {
			continue
		}
		if len(attentionSet) > 0 && !attentionSet[item.AttentionStage] {
			continue
		}
		if len(groupSet) > 0 && !groupSet[item.Group] {
			continue
		}
		if !hasAllTags(item.Tags, opts.Tags) {
			continue
		}
		if len(projectSet) > 0 && !anyMatches(item.Projects, projectSet) {
			continue
		}
		if !opts.ShowParked && len(attentionSet) == 0 && len(statusSet) == 0 && item.AttentionStage == "parked" {
			continue
		}
		if query != "" && !matchesQuery(item, query) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func normalizedStatusSet(vals []string) map[string]bool {
	set := make(map[string]bool, len(vals))
	for _, value := range vals {
		set[NormalizeDisplayStatus(value)] = true
	}
	if len(set) == 0 {
		return nil
	}
	return set
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

// hasAllTags reports whether item carries every tag in wanted (AND semantics,
// deliberately different from every other filter dimension in this file, which
// are OR-within-dimension). Returns true when wanted is empty.
func hasAllTags(itemTags, wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	set := toSet(itemTags)
	for _, w := range wanted {
		if !set[w] {
			return false
		}
	}
	return true
}

// anyMatches reports whether any of items intersects wantedSet (OR semantics,
// matching the convention used by Types/Categories/etc in this file).
func anyMatches(items []string, wantedSet map[string]bool) bool {
	for _, v := range items {
		if wantedSet[v] {
			return true
		}
	}
	return false
}

func matchesQuery(item WorkItem, query string) bool {
	return strings.Contains(strings.ToLower(item.Title), query) ||
		strings.Contains(strings.ToLower(item.RelativePath), query) ||
		strings.Contains(strings.ToLower(item.Summary), query) ||
		strings.Contains(strings.ToLower(item.SourceID), query) ||
		strings.Contains(strings.ToLower(item.Group), query) ||
		strings.Contains(strings.ToLower(item.WorkflowCategory), query)
}
