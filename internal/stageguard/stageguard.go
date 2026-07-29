// Package stageguard decides what camp's stage-everything operations may
// stage. It is a leaf package: internal/git and pkg/commitkit both import
// it, so it must import neither.
//
// The guard answers one question -- "what would a stage-everything operation
// add here, and is any of it a mistake" -- and returns the answer as typed
// data. Rendering that data (error copy, remedy menus, --json shapes) belongs
// to the CLI layer, and fest renders the same values its own way.
package stageguard

import "context"

// Mode selects how a guard responds when it finds something.
type Mode string

const (
	// ModeAuto declares an artifact root, excludes the file, and reports.
	// Valid for the large-file guard only.
	ModeAuto Mode = "auto"
	// ModeBlock refuses the operation and stages nothing.
	ModeBlock Mode = "block"
	// ModeOff disables detection entirely.
	ModeOff Mode = "off"
)

// Valid reports whether m is a mode this package recognizes.
func (m Mode) Valid() bool {
	switch m {
	case ModeAuto, ModeBlock, ModeOff:
		return true
	}
	return false
}

// GuardLimits is the resolved policy for one repository. ResolveLimits builds
// it from campaign and project config; callers that want to ask "what would
// happen under these limits" can construct one directly.
type GuardLimits struct {
	// MaxFileSize is the per-file threshold in bytes. The campaign root and a
	// project submodule carry different defaults.
	MaxFileSize int64
	// MaxAddedFiles is the bulk trigger: untracked files under a dominant
	// prefix. There is deliberately no total-bytes leg.
	MaxAddedFiles int
	// Allow lists globs exempting paths from every guard, permanently.
	Allow []string
	// LargeFiles is the commit.guards.large_files mode.
	LargeFiles Mode
	// Bulk is the commit.guards.bulk mode: block or off, never auto.
	Bulk Mode
	// ScopeProject is true when the repository is a project rather than the
	// campaign root. It selects which remedy the CLI offers, since camp never
	// auto-declares an artifact root inside a project.
	ScopeProject bool
}

// ViolationKind distinguishes the guard that produced a violation, because
// each carries a different remedy.
type ViolationKind string

const (
	// OverThreshold is an untracked file above MaxFileSize. At the campaign
	// root it is an auto-declare candidate; inside a project it is
	// exclude-and-ask.
	OverThreshold ViolationKind = "over_threshold"
	// Bulk is many untracked files under one prefix. It always blocks unless
	// the bulk mode is off, because camp cannot infer whether the tree wants
	// gitignore or an artifact root.
	Bulk ViolationKind = "bulk"
	// TrackedGrowth is a tracked file whose new content exceeds MaxFileSize.
	// It is reported every time and excluded never: splitting one file's
	// history between git and an artifact root is never correct.
	TrackedGrowth ViolationKind = "tracked_growth"
)

// GuardViolation is one finding. Prefix, Count, and TotalBytes are set for
// Bulk only; Path and Size are set for the per-file kinds.
type GuardViolation struct {
	Kind ViolationKind
	// Path is repo-relative and slash-separated.
	Path string
	// Size is the file size in bytes, from lstat (symlinks are never followed).
	Size int64
	// CommonPrefix is the dominant directory a Bulk violation was found under.
	CommonPrefix string
	// Count is how many untracked files a Bulk violation covers.
	Count int
	// TotalBytes is the size of a Bulk violation's files. It is reported for
	// display and is never itself a trigger.
	TotalBytes int64
}

// ReportOnly reports whether a violation must never cause exclusion. Tracked
// growth is always committed; the user is told about it instead.
func (v GuardViolation) ReportOnly() bool {
	return v.Kind == TrackedGrowth
}

// Blocks reports whether a violation stops the operation outright under
// limits, as opposed to being handled by exclusion or a report.
func (v GuardViolation) Blocks(limits GuardLimits) bool {
	switch v.Kind {
	case Bulk:
		return limits.Bulk == ModeBlock
	case OverThreshold:
		return limits.LargeFiles == ModeBlock
	default:
		return false
	}
}

// Check resolves limits for repoPath and evaluates the guards against it. It
// is the entry point for the chokepoint in internal/git.Stage, which has no
// config knowledge of its own.
func Check(ctx context.Context, repoPath string) ([]GuardViolation, GuardLimits, error) {
	limits, err := ResolveLimits(ctx, repoPath)
	if err != nil {
		return nil, GuardLimits{}, err
	}
	violations, err := CheckStaging(ctx, repoPath, limits)
	if err != nil {
		return nil, limits, err
	}
	return violations, limits, nil
}
