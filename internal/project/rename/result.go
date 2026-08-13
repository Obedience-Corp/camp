// Package rename implements the transactional project rename operation.
package rename

import "context"

// Kind identifies how a managed project is represented by the campaign.
type Kind string

const (
	KindSubmodule   Kind = "submodule"
	KindLinked      Kind = "linked"
	KindCampaignDir Kind = "campaign-directory"
)

// Options controls planning and application of a project rename.
type Options struct {
	RemoteURL    string
	VerifyRemote bool
	DryRun       bool
}

// WorktreeChange describes a linked worktree affected by the rename.
type WorktreeChange struct {
	Before   string `json:"before"`
	After    string `json:"after"`
	Moved    bool   `json:"moved"`
	External bool   `json:"external"`
	Dirty    bool   `json:"dirty"`
}

// MetadataChange describes a typed Camp store that contains active identity
// references which will be migrated.
type MetadataChange struct {
	Store   string `json:"store"`
	Path    string `json:"path"`
	Records int    `json:"records"`
}

// PlanResult is the immutable, user-visible plan produced before mutation.
type PlanResult struct {
	OperationID string `json:"operation_id"`
	Kind        Kind   `json:"kind"`
	OldName     string `json:"old_name"`
	NewName     string `json:"new_name"`
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	OldURL      string `json:"old_url,omitempty"`
	NewURL      string `json:"new_url,omitempty"`

	CampaignRoot         string           `json:"-"`
	ProjectsRoot         string           `json:"-"`
	SubmoduleSection     string           `json:"-"`
	Initialized          bool             `json:"initialized"`
	LinkedTarget         string           `json:"linked_target,omitempty"`
	Worktrees            []WorktreeChange `json:"worktrees,omitempty"`
	Metadata             []MetadataChange `json:"metadata,omitempty"`
	CommitFiles          []string         `json:"commit_files,omitempty"`
	AutoCommitEligible   bool             `json:"auto_commit_eligible"`
	AutoCommitSkipReason string           `json:"auto_commit_skip_reason,omitempty"`
}

// Reference is a tracked, intentionally retained historical reference.
type Reference struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
}

// Result reports the verified operation and any non-fatal warnings.
type Result struct {
	Plan               *PlanResult `json:"plan"`
	Steps              []string    `json:"steps"`
	Verified           bool        `json:"verified"`
	RolledBack         bool        `json:"rolled_back"`
	RecoveryJournal    string      `json:"recovery_journal,omitempty"`
	Warnings           []string    `json:"warnings,omitempty"`
	ResidualReferences []Reference `json:"residual_references,omitempty"`
}

// Planner is useful at command boundaries and in tests.
type Planner interface {
	Plan(context.Context, string, string, string, Options) (*PlanResult, error)
}
