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

	StableID     string            `json:"stable_id,omitempty"`
	WorkflowMeta *WorkItemWorkflow `json:"workflow,omitempty"`
}

// WorkItemWorkflow carries local runtime progress when .workflow/ is present
// (sourced from the fest local runtime, populated by camp's localrun loader).
type WorkItemWorkflow struct {
	WorkflowID     string `json:"workflow_id,omitempty"`
	ActiveRunID    string `json:"active_run_id,omitempty"`
	CurrentStep    int    `json:"current_step,omitempty"`
	TotalSteps     int    `json:"total_steps,omitempty"`
	CompletedSteps int    `json:"completed_steps,omitempty"`
	RunStatus      string `json:"run_status,omitempty"`
	Blocked        bool   `json:"blocked,omitempty"`
	DocHashChanged bool   `json:"doc_hash_changed,omitempty"`
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
