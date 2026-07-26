package commitkit

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// The staging guard's public surface, re-exported for consumers outside this
// module. fest is a separate module and cannot import camp's internal/
// packages, so this file is the contract it codes against; only the
// implementation lives behind it.
//
// Almost none of this needs calling. The guard runs inside git.Stage, which
// every camp staging path funnels through, and it resolves its own thresholds
// from repoPath. A consumer that stages through Stage, StageAll, StageFiles,
// or CommitAll therefore inherits the guard from a camp version bump with no
// code change at all. What is here is for the cases that want to look before
// staging, or to render violations their own way.
//
// Deliberately absent: DrainJobs, which flushes deferred commit jobs. It needs
// the job queue that phase 002 of this work introduces, so it is added with
// the queue rather than stubbed here. See the deferred-commits design under
// workflow/design/camp-artifact-commit-updates-2026-07-25/deferred-commits/.

// GuardLimits is the staging policy for one repository: per-file size
// threshold, bulk trigger, allowlist, and the mode of each guard.
//
// It is an alias rather than a distinct type, so the values a consumer builds
// and the values the guard returns are the same type and camp never converts
// between them.
type GuardLimits = stageguard.GuardLimits

// GuardViolation is one finding from the guard: an over-threshold file, a
// tracked file that grew past the threshold, or a bulk directory.
//
// Violations are data, not an error string to parse. Camp's CLI renders them
// one way and a consumer is free to render them another; both switch on Kind
// rather than matching text.
type GuardViolation = stageguard.GuardViolation

// ViolationKind distinguishes which guard produced a violation. Each kind
// carries a different remedy, so consumers switch on it to decide what to say.
type ViolationKind = stageguard.ViolationKind

// The violation kinds a consumer can encounter.
const (
	// OverThreshold is an untracked file above the size threshold. At a
	// campaign root camp declares an artifact root for it and excludes it;
	// inside a project camp excludes it and asks, because an artifact root
	// would keep the bytes off the project's remote.
	OverThreshold = stageguard.OverThreshold
	// Bulk is many untracked files under one directory. It blocks: camp
	// cannot tell a vendor tree that wants gitignore from a photo library
	// that wants an artifact root.
	Bulk = stageguard.Bulk
	// TrackedGrowth is a tracked file whose new content crossed the
	// threshold. It is always reported and never excluded, because splitting
	// one file's history between git and an artifact root is never correct.
	TrackedGrowth = stageguard.TrackedGrowth
)

// Mode is a guard's configured behavior.
type Mode = stageguard.Mode

// The guard modes. ModeAuto applies to the large-file guard only; the bulk
// guard accepts ModeBlock or ModeOff, since there is no inference camp could
// make on its behalf.
const (
	ModeAuto  = stageguard.ModeAuto
	ModeBlock = stageguard.ModeBlock
	ModeOff   = stageguard.ModeOff
)

// CheckStaging reports what a stage-everything operation in repoPath would add
// that the guard would act on. It is stat-only: nothing is opened or hashed.
//
// This is a preflight call, and ordinary staging does not need it. Stage,
// StageAll, and StageFiles already run the guard internally with limits
// resolved from repoPath, so a consumer that only wants the protection should
// call nothing. Use CheckStaging to ask "what would happen here" without
// staging, or to render the findings before deciding.
//
// The limits passed here override the ones resolution would produce for
// repoPath. To ask under the repository's own configured policy, get them from
// ResolveLimits first.
func CheckStaging(ctx context.Context, repoPath string, limits GuardLimits) ([]GuardViolation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return stageguard.CheckStaging(ctx, repoPath, limits)
}

// ResolveLimits loads the guard policy that applies to repoPath, merging
// campaign and per-project configuration over camp's shipped defaults. Outside
// a campaign it returns the project-scope defaults, since a bare git repo is
// closer to a project than to a campaign root.
//
// Pair it with CheckStaging to preflight under the repository's real policy.
func ResolveLimits(ctx context.Context, repoPath string) (GuardLimits, error) {
	if ctx.Err() != nil {
		return GuardLimits{}, ctx.Err()
	}
	return stageguard.ResolveLimits(ctx, repoPath)
}
