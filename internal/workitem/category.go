package workitem

// ApplyWorkflowCategories sets WorkflowCategory on each item from categoryForType,
// which maps a workflow type key to a category key. The category is derived at
// read time from campaign config; enrichment never changes which items exist or
// any other field. Items are enriched in place and the same slice is returned.
func ApplyWorkflowCategories(items []WorkItem, categoryForType func(string) string) []WorkItem {
	if categoryForType == nil {
		return items
	}
	for i := range items {
		items[i].WorkflowCategory = categoryForType(string(items[i].WorkflowType))
	}
	return items
}
