package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// The staging guard's attachment point. internal/git may import stageguard;
// the leaf constraint only forbids the reverse edge, since stageguard running
// its own git status is what keeps it importable from both sides.
//
// The guard runs before executeStage, never after. Staging first and cleaning
// up afterward would leave a window where the index holds bytes camp has
// already decided do not belong in the commit, and a failure in that window
// would leave it there.

// StageOptions carries per-invocation overrides of the guard's decision.
type StageOptions struct {
	// CommitLarge forces over-threshold files and bulk directories into the
	// index for this invocation, and declares nothing.
	//
	// The name is the inverse of what it would have been under a blocking
	// guard. With auto-handling, the thing a user needs to override is camp's
	// decision to EXCLUDE ("no, this release binary belongs in git"), so
	// naming it --allow-large would read as "skip the safety check", which is
	// backwards.
	CommitLarge bool
}

// StageOutcome is what the guard decided about a staging operation, returned
// so the CLI can report it. Rendering belongs to the caller: camp's commit
// command writes remedy menus, fest renders the same values its own way.
type StageOutcome struct {
	// Excluded holds over-threshold untracked files kept out of the index.
	Excluded []stageguard.GuardViolation
	// Reported holds tracked files whose new content crossed the threshold.
	// They are always staged; the user is told about them instead.
	Reported []stageguard.GuardViolation
	// Limits is the policy that produced this outcome, so callers can render
	// the threshold they were measured against without resolving it again.
	Limits stageguard.GuardLimits
}

// Empty reports whether the guard found nothing worth telling the user about.
func (o *StageOutcome) Empty() bool {
	return o == nil || (len(o.Excluded) == 0 && len(o.Reported) == 0)
}

// ExcludedPaths returns the repo-relative paths kept out of the index.
func (o *StageOutcome) ExcludedPaths() []string {
	if o == nil {
		return nil
	}
	paths := make([]string, 0, len(o.Excluded))
	for _, v := range o.Excluded {
		paths = append(paths, v.Path)
	}
	return paths
}

// GuardBlockedError reports that the guard refused a staging operation. It
// carries the violations as typed data so no caller ever parses a message to
// find out what happened.
type GuardBlockedError struct {
	// Kind is the guard that refused: stageguard.Bulk or
	// stageguard.OverThreshold (the latter only in block mode).
	Kind       stageguard.ViolationKind
	Violations []stageguard.GuardViolation
	Limits     stageguard.GuardLimits
	// RepoPath is the repository the refusal applies to.
	RepoPath string
}

func (e *GuardBlockedError) Error() string {
	switch e.Kind {
	case stageguard.Bulk:
		for _, v := range e.Violations {
			if v.Kind == stageguard.Bulk {
				return "staging refused: " + v.CommonPrefix + " would add " +
					itoa(v.Count) + " untracked files"
			}
		}
		return "staging refused: bulk directory"
	default:
		paths := make([]string, 0, len(e.Violations))
		for _, v := range e.Violations {
			paths = append(paths, v.Path)
		}
		return "staging refused: files over the size limit: " + strings.Join(paths, ", ")
	}
}

// itoa avoids pulling strconv in for one call site in an error path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// isStageEverything reports whether a files argument means "stage the whole
// worktree" rather than an explicit list the user named.
//
// The distinction is the design's: an explicit `git add path/to/media.mp4` is
// an intent, and camp honors it. Only the sweep forms are guarded, because
// only they can pick up something the user never mentioned.
//
// StageAllExcluding passes "--", "." plus exclusion pathspecs, which is a
// sweep and must be guarded; missing that would leave the campaign root's
// commit path (which always excludes submodule refs) unguarded.
func isStageEverything(files []string) bool {
	if len(files) == 0 {
		return true
	}
	for _, f := range files {
		switch {
		case f == "--" || f == "." || strings.HasPrefix(f, ":(exclude"):
			continue
		default:
			return false
		}
	}
	return true
}

// runStageGuard evaluates the guard for a staging operation and returns both
// the outcome and the pathspec exclusions to apply.
//
// It is deliberately fail-closed on refusals and fail-open on its own errors:
// a repository the guard cannot read (not a repo yet, git unavailable) must
// still be stageable, because the guard is a safety net rather than a
// precondition for camp working at all.
func runStageGuard(ctx context.Context, repoPath string, files []string, opts StageOptions) (*StageOutcome, []string, error) {
	if !isStageEverything(files) {
		return nil, nil, nil
	}
	// --commit-large is the user overruling camp's decision to exclude, which
	// is the thing that actually needs an override once handling is automatic.
	// It suppresses detection entirely rather than excluding-then-restoring,
	// so the index ends up exactly as an unguarded stage would leave it.
	if opts.CommitLarge {
		return nil, nil, nil
	}

	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		return nil, nil, camperrors.Wrapf(err, "resolve staging guard limits for %s", repoPath)
	}
	if limits.LargeFiles == stageguard.ModeOff && limits.Bulk == stageguard.ModeOff {
		return nil, nil, nil
	}

	violations, err := stageguard.CheckStaging(ctx, repoPath, limits)
	if err != nil {
		return nil, nil, camperrors.Wrapf(err, "evaluate staging guard for %s", repoPath)
	}
	if len(violations) == 0 {
		return nil, nil, nil
	}

	outcome := &StageOutcome{Limits: limits}
	var bulk []stageguard.GuardViolation
	var overThreshold []stageguard.GuardViolation

	for _, v := range violations {
		switch v.Kind {
		case stageguard.Bulk:
			bulk = append(bulk, v)
		case stageguard.TrackedGrowth:
			outcome.Reported = append(outcome.Reported, v)
		case stageguard.OverThreshold:
			overThreshold = append(overThreshold, v)
		}
	}

	// Bulk is checked first: it refuses the whole operation, so there is no
	// point deciding exclusions the caller will never apply.
	if len(bulk) > 0 && limits.Bulk == stageguard.ModeBlock {
		return nil, nil, &GuardBlockedError{
			Kind: stageguard.Bulk, Violations: bulk, Limits: limits, RepoPath: repoPath,
		}
	}
	if len(overThreshold) > 0 && limits.LargeFiles == stageguard.ModeBlock {
		return nil, nil, &GuardBlockedError{
			Kind: stageguard.OverThreshold, Violations: overThreshold, Limits: limits, RepoPath: repoPath,
		}
	}

	outcome.Excluded = overThreshold
	return outcome, outcome.ExcludedPaths(), nil
}

// applyGuardExclusions folds guard exclusions into the pathspec, composing
// with any exclusions the caller already built (the campaign root always
// excludes submodule refs) so staging stays one git invocation.
func applyGuardExclusions(files []string, excluded []string) []string {
	if len(excluded) == 0 {
		return files
	}
	if len(files) == 0 {
		files = []string{"--", "."}
	}
	for _, p := range excluded {
		files = append(files, ":(exclude,literal)"+p)
	}
	return files
}

// StagedPathsUnder returns the staged paths beneath a repo-relative directory.
// Used to report which files inside a newly declared artifact root still
// belong to git, which is the difference between telling a user their notes
// are versioned and leaving them to assume otherwise.
func StagedPathsUnder(ctx context.Context, repoPath, dir string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"diff", "--cached", "--name-only", "-z", "--", dir)
	cmd.Env = gitEnv(os.Environ())

	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "list staged paths under %s", dir)
	}

	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	paths := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 0 {
			paths = append(paths, string(f))
		}
	}
	return paths, nil
}
