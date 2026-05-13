package workitem

// ApplyMetadata merges parsed .workitem metadata into a derived WorkItem.
//
// The derived item already has path-based fields populated (Key, WorkflowType,
// LifecycleStage from placement, RelativePath, PrimaryDoc, CreatedAt,
// UpdatedAt). ApplyMetadata layers metadata-derived fields on top, respecting
// the precedence rules in WORKITEM_CONTRACT.md "Path Semantics" and
// "Status Rules":
//
//   - filesystem placement wins for LifecycleStage and RelativePath
//   - metadata wins for StableID, Description, Execution, PriorityInfo,
//     Project, WorkflowMeta, Lineage
//   - Title is overridden only when metadata supplies a non-empty value
//
// Returns the WorkItem by value so callers can replace it cleanly.
func ApplyMetadata(item WorkItem, md *Metadata) WorkItem {
	if md == nil {
		return item
	}

	item.StableID = md.ID
	if md.Title != "" {
		item.Title = md.Title
	}
	if md.Description != "" {
		item.Description = md.Description
	}

	if md.Execution != nil {
		item.Execution = &WorkItemExecution{
			Mode:          md.Execution.Mode,
			Autonomy:      md.Execution.Autonomy,
			Risk:          md.Execution.Risk,
			BlockedReason: md.Execution.BlockedReason,
		}
	}
	if md.Priority != nil {
		item.PriorityInfo = &WorkItemPriority{
			Level:  md.Priority.Level,
			Reason: md.Priority.Reason,
		}
	}
	if md.Project != nil {
		item.Project = &WorkItemProject{
			Name: md.Project.Name,
			Path: md.Project.Path,
			Role: md.Project.Role,
		}
	}
	if md.Workflow != nil {
		item.WorkflowMeta = &WorkItemWorkflow{
			DocPath:     md.Workflow.DocPath,
			RuntimeDir:  md.Workflow.RuntimeDir,
			WorkflowID:  md.Workflow.WorkflowID,
			ActiveRunID: md.Workflow.ActiveRunID,
		}
	}
	if md.Lineage != nil {
		item.Lineage = &WorkItemLineage{
			PromotedFrom: md.Lineage.PromotedFrom,
			PromotedTo:   md.Lineage.PromotedTo,
			Supersedes:   md.Lineage.Supersedes,
		}
	}

	return item
}
