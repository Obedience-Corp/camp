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
	Key                  string         `json:"key"`
	WorkflowType         WorkflowType   `json:"workflow_type"`
	WorkflowCategory     string         `json:"workflow_category,omitempty"`
	LifecycleStage       LifecycleStage `json:"lifecycle_stage"`
	Title                string         `json:"title"`
	RelativePath         string         `json:"relative_path"`
	PrimaryDoc           string         `json:"primary_doc"`
	ItemKind             ItemKind       `json:"item_kind"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	SortTimestamp        time.Time      `json:"sort_timestamp"`
	ManualPriority       string         `json:"manual_priority,omitempty"`
	AttentionStage       string         `json:"attention_stage,omitempty"`
	AttentionStageSource string         `json:"attention_stage_source,omitempty"`
	Group                string         `json:"group,omitempty"`
	Summary              string         `json:"summary"`
	SourceID             string         `json:"source_id"`
	SourceMetadata       map[string]any `json:"source_metadata"`

	StableID     string            `json:"stable_id,omitempty"`
	WorkflowMeta *WorkItemWorkflow `json:"workflow,omitempty"`
	Tags         []string          `json:"tags"`
	Projects     []string          `json:"-"`
	ProjectLinks []ProjectLink     `json:"-"`
	ProjectRefs  []ProjectRef      `json:"projects"`
	TokenCount   int               `json:"token_count,omitempty"`
	Completion   *CompletionState  `json:"completion,omitempty"`
}

// CompletionState is emitted only when a workitem carries a non-default
// completion decision. Policy is normalized to review or recurring.
type CompletionState struct {
	Policy        CompletionPolicy `json:"policy"`
	ReviewedRunID string           `json:"reviewed_run_id,omitempty"`
}

// ProjectLink is a project a workitem reaches through links.yaml rather than
// through its own projects: list. Worktree is the campaign-relative worktree
// path that carries the relationship, empty when the link points straight at
// the project. A worktree is a checkout of a project, so the project is the
// subject of the relationship and the worktree is a detail of it, which is why
// this resolves rather than reporting the worktree path as a project.
//
// The field is populated at read time from the registry and is never persisted
// on the workitem.
type ProjectLink struct {
	Path     string
	Worktree string
	Primary  bool
}

// ProjectRef is one entry in a workitem's merged projects view: a
// campaign-relative project path, annotated with whether that project is also
// the workitem-scope primary link in links.yaml. It is the JSON shape of the
// "projects" field (workitems/v1alpha9); the plain []string Projects field
// stays the internal semantic base that ApplyMetadata populates and the merged
// view is derived from at output time.
//
// Worktree and WorktreeMissing are additive: they name the worktree a
// link-derived project came through and report whether that directory is still
// on this machine. Primary keeps its original meaning, a project-scope primary
// link on this exact path, so an existing reader sees no value change.
type ProjectRef struct {
	Path            string `json:"path"`
	Primary         bool   `json:"primary"`
	Worktree        string `json:"worktree,omitempty"`
	WorktreeMissing bool   `json:"worktree_missing,omitempty"`
}

// WorkItemWorkflow carries local runtime progress when .workflow/ is present
// (sourced from the fest local runtime, populated by camp's localrun loader).
type WorkItemWorkflow struct {
	WorkflowID  string `json:"workflow_id,omitempty"`
	ActiveRunID string `json:"active_run_id,omitempty"`
	// LatestRunID / LatestRunStatus report the most recent run's terminal state
	// when there is no active run (fest clears active_run_id on completion).
	// Additive JSON fields (omitempty); existing readers ignore them.
	LatestRunID     string `json:"latest_run_id,omitempty"`
	LatestRunStatus string `json:"latest_run_status,omitempty"`
	CurrentStep     int    `json:"current_step"`
	TotalSteps      int    `json:"total_steps"`
	CompletedSteps  int    `json:"completed_steps"`
	RunStatus       string `json:"run_status,omitempty"`
	Blocked         bool   `json:"blocked"`
	DocHashChanged  bool   `json:"doc_hash_changed"`
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
