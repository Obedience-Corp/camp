// Package workitem provides a normalized model for campaign work items across
// intents, design docs, explore items, and festivals. This shared model is
// consumed by both the --json CLI output and the TUI dashboard.
//
// All paths on WorkItem are campaign-relative. Use AbsPath() and AbsPrimaryDoc()
// to resolve absolute paths at the point of use.
package workitem

import (
	"path/filepath"
	"time"
)

// WorkflowType identifies which campaign surface a work item belongs to.
type WorkflowType string

const (
	WorkflowTypeIntent   WorkflowType = "intent"
	WorkflowTypeDesign   WorkflowType = "design"
	WorkflowTypeExplore  WorkflowType = "explore"
	WorkflowTypeFestival WorkflowType = "festival"
)

// ItemKind distinguishes file-based items from directory-based items.
type ItemKind string

const (
	ItemKindFile      ItemKind = "file"
	ItemKindDirectory ItemKind = "directory"
)

// WorkItem is the normalized model shared by --json output and the TUI dashboard.
// All path fields are campaign-relative. The campaign root is the boundary.
type WorkItem struct {
	Key            string         `json:"key"`
	WorkflowType   WorkflowType   `json:"workflow_type"`
	LifecycleStage string         `json:"lifecycle_stage"`
	Title          string         `json:"title"`
	RelativePath   string         `json:"relative_path"`
	PrimaryDoc     string         `json:"primary_doc"`
	ItemKind       ItemKind       `json:"item_kind"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	SortTimestamp  time.Time      `json:"sort_timestamp"`
	ManualPriority string         `json:"manual_priority,omitempty"`
	Summary        string         `json:"summary"`
	SourceID       string         `json:"source_id"`
	SourceMetadata map[string]any `json:"source_metadata"`

	// Metadata surface (populated when a .workitem file is present and parses).
	// All fields use omitempty so legacy items without metadata serialize byte-identically.
	StableID     string             `json:"stable_id,omitempty"`
	Description  string             `json:"description,omitempty"`
	Execution    *WorkItemExecution `json:"execution,omitempty"`
	PriorityInfo *WorkItemPriority  `json:"priority_info,omitempty"`
	Project      *WorkItemProject   `json:"project,omitempty"`
	WorkflowMeta *WorkItemWorkflow  `json:"workflow,omitempty"`
	Lineage      *WorkItemLineage   `json:"lineage,omitempty"`
}

// WorkItemExecution mirrors the .workitem execution block.
type WorkItemExecution struct {
	Mode          string `json:"mode,omitempty"`
	Autonomy      string `json:"autonomy,omitempty"`
	Risk          string `json:"risk,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// WorkItemPriority mirrors the .workitem priority block.
type WorkItemPriority struct {
	Level  string `json:"level,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// WorkItemProject mirrors the .workitem project block.
type WorkItemProject struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	Role string `json:"role,omitempty"`
}

// WorkItemWorkflow mirrors the .workitem workflow block, plus local
// runtime progress when .workflow/ is present (added in WW0001/005.01).
type WorkItemWorkflow struct {
	DocPath     string `json:"doc_path,omitempty"`
	RuntimeDir  string `json:"runtime_dir,omitempty"`
	WorkflowID  string `json:"workflow_id,omitempty"`
	ActiveRunID string `json:"active_run_id,omitempty"`

	// Local runtime progress (populated when .workflow/workflow.yaml exists).
	CurrentStep    int    `json:"current_step,omitempty"`
	TotalSteps     int    `json:"total_steps,omitempty"`
	CompletedSteps int    `json:"completed_steps,omitempty"`
	RunStatus      string `json:"run_status,omitempty"`
	Blocked        bool   `json:"blocked,omitempty"`
	DocHashChanged bool   `json:"doc_hash_changed,omitempty"`
}

// WorkItemLineage mirrors the .workitem lineage block.
type WorkItemLineage struct {
	PromotedFrom []string `json:"promoted_from,omitempty"`
	PromotedTo   []string `json:"promoted_to,omitempty"`
	Supersedes   []string `json:"supersedes,omitempty"`
}

// AbsPath resolves the item's absolute path from the campaign root.
func (w WorkItem) AbsPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, w.RelativePath)
}

// AbsPrimaryDoc resolves the primary doc's absolute path, or empty if none.
func (w WorkItem) AbsPrimaryDoc(campaignRoot string) string {
	if w.PrimaryDoc == "" {
		return ""
	}
	return filepath.Join(campaignRoot, w.PrimaryDoc)
}

// DeriveSortTimestamp returns updated_at if non-zero, else created_at.
func DeriveSortTimestamp(updated, created time.Time) time.Time {
	if !updated.IsZero() {
		return updated
	}
	return created
}
