package workitem

func ApplyWorkflowCategories(items []WorkItem, categoryForType func(string) string) []WorkItem {
	if categoryForType == nil {
		return items
	}
	for i := range items {
		items[i].WorkflowCategory = categoryForType(string(items[i].WorkflowType))
	}
	return items
}
