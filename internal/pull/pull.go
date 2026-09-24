// Package pull contains pull orchestration for campaign repositories.
package pull

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// IO configures process streams for git pull execution.
type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Options configures a pull-all operation.
type Options struct {
	NoRecurse     bool
	DefaultBranch bool
	// Parallel bounds how many submodules pull at once; below 1 means DefaultParallel.
	Parallel int
	IO       IO
}

// Target is one repository that may be pulled.
type Target struct {
	Name    string
	Path    string
	RelPath string
	Branch  string
	IsRoot  bool
}

// Outcome is the result category for a target.
type Outcome int

const (
	OutcomePulled Outcome = iota
	OutcomeSkipped
	OutcomeFailed
)

// Result carries one target pull outcome.
type Result struct {
	Target         Target
	Outcome        Outcome
	Status         string
	ErrorMessage   string
	OriginalBranch string
}

// Summary carries aggregate pull-all results.
type Summary struct {
	Pulled      int
	Skipped     int
	Failed      int
	Errors      []string
	ChangedRefs []string
	Results     []Result
}

// Hooks let callers render progress without putting UI dependencies in this package.
//
// OnBegin and OnFinish fire from worker goroutines as each target starts and
// finishes, in whatever order that happens, so they must be safe for concurrent
// use. Every other hook fires on the calling goroutine, and OnSkip, OnPulling,
// and OnResult fire in declaration order.
type Hooks struct {
	OnStart       func()
	OnTargets     func([]Target)
	OnBegin       func(Target)
	OnFinish      func(Result)
	OnSkip        func(Target, string)
	OnPulling     func(Target, string)
	OnResult      func(Result)
	OnChangedRefs func([]string)
	OnSummary     func(Summary)
}

// RunGitPullWithLockRetry runs git pull with lock retry behavior.
func RunGitPullWithLockRetry(ctx context.Context, repoPath string, gitArgs []string, stream bool, streams IO) ([]byte, error) {
	cfg := git.DefaultRetryConfig()
	cfg.OperationName = "pull"

	var output []byte
	err := git.WithLockRetry(ctx, repoPath, cfg, func() error {
		pullArgs := append([]string{"-C", repoPath, "pull"}, gitArgs...)
		gitCmd := exec.CommandContext(ctx, "git", pullArgs...)
		if streams.Stdin != nil {
			gitCmd.Stdin = streams.Stdin
		}

		var err error
		if stream {
			var stderr bytes.Buffer
			if streams.Stdout != nil {
				gitCmd.Stdout = streams.Stdout
			}
			if streams.Stderr != nil {
				gitCmd.Stderr = io.MultiWriter(streams.Stderr, &stderr)
			} else {
				gitCmd.Stderr = &stderr
			}
			err = gitCmd.Run()
			output = stderr.Bytes()
		} else {
			output, err = gitCmd.CombinedOutput()
		}
		if err != nil {
			return classifyCommandError(repoPath, output, err)
		}
		return nil
	})
	return output, err
}

func classifyCommandError(repoPath string, output []byte, err error) error {
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	if git.ClassifyGitError(string(output), exitCode) == git.GitErrorLock {
		lockPath := "index.lock"
		if gitDir, resolveErr := git.ResolveGitDir(repoPath); resolveErr == nil {
			lockPath = filepath.Join(gitDir, "index.lock")
		}
		return &git.LockError{Path: lockPath, Err: err}
	}
	return err
}

// RunAll discovers all submodules plus the campaign root, and pulls them.
func RunAll(ctx context.Context, campRoot string, gitArgs []string, opts Options, hooks Hooks) (Summary, error) {
	if hooks.OnStart != nil {
		hooks.OnStart()
	}

	paths, err := discoverSubmodules(ctx, campRoot, opts.NoRecurse)
	if err != nil {
		return Summary{}, err
	}

	targets := buildTargets(ctx, campRoot, paths)
	if hooks.OnTargets != nil {
		hooks.OnTargets(targets)
	}

	run := func(t *Target) attempt {
		if hooks.OnBegin != nil {
			hooks.OnBegin(*t)
		}
		a := pullTarget(ctx, t, gitArgs, opts)
		if hooks.OnFinish != nil {
			hooks.OnFinish(a.result)
		}
		return a
	}

	var summary Summary
	record := func(result Result) {
		summary.Results = append(summary.Results, result)
		switch result.Outcome {
		case OutcomePulled:
			summary.Pulled++
		case OutcomeSkipped:
			summary.Skipped++
		case OutcomeFailed:
			summary.Failed++
			summary.Errors = append(summary.Errors, result.ErrorMessage)
		}
	}

	if ctx.Err() != nil {
		return summary, ctx.Err()
	}
	record(run(&targets[0]).report(hooks))

	subs := targets[1:]
	attempts := make([]attempt, len(subs))
	err = runOrdered(ctx, len(subs), opts.Parallel,
		func(i int) { attempts[i] = run(&subs[i]) },
		func(i int) { record(attempts[i].report(hooks)) },
	)
	if err != nil {
		return summary, err
	}

	summary.ChangedRefs = ChangedRefs(ctx, campRoot, paths)
	if hooks.OnChangedRefs != nil {
		hooks.OnChangedRefs(summary.ChangedRefs)
	}
	if hooks.OnSummary != nil {
		hooks.OnSummary(summary)
	}

	if summary.Failed > 0 {
		return summary, camperrors.New(fmt.Sprintf("%d repo(s) failed to pull", summary.Failed))
	}
	return summary, nil
}

// PullTarget handles checkout-if-needed, upstream checks, and pull for one target.
func PullTarget(ctx context.Context, t *Target, gitArgs []string, opts Options, hooks Hooks) Result {
	return pullTarget(ctx, t, gitArgs, opts).report(hooks)
}

// attempt is one target's outcome, held until it is reported in declaration order.
type attempt struct {
	result         Result
	originalBranch string
	pulled         bool
}

func (a attempt) report(hooks Hooks) Result {
	if !a.pulled {
		if hooks.OnSkip != nil {
			hooks.OnSkip(a.result.Target, a.result.Status)
		}
		return a.result
	}
	if hooks.OnPulling != nil {
		hooks.OnPulling(a.result.Target, a.originalBranch)
	}
	if hooks.OnResult != nil {
		hooks.OnResult(a.result)
	}
	return a.result
}

func pullTarget(ctx context.Context, t *Target, gitArgs []string, opts Options) attempt {
	originalBranch := t.Branch

	if git.IsRebaseInProgress(ctx, t.Path) {
		return skipped(*t, "rebase in progress -- resolve or abort manually")
	}

	if t.Branch == "" || t.Branch == "HEAD" {
		if opts.DefaultBranch && !t.IsRoot {
			if _, _, err := checkoutDefaultIfNeeded(ctx, t); err != nil {
				return skipped(*t, "detached HEAD (checkout failed)")
			}
		} else {
			return skipped(*t, "detached HEAD")
		}
	}

	if _, err := git.Output(ctx, t.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		if opts.DefaultBranch && !t.IsRoot {
			branch, checkoutErr := git.CheckoutDefaultBranch(ctx, t.Path)
			if checkoutErr != nil {
				return skipped(*t, "no upstream (checkout failed)")
			}
			if t.Branch != branch {
				t.Branch = branch
			}
			if _, err := git.Output(ctx, t.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
				return skipped(*t, "no upstream")
			}
		} else {
			return skipped(*t, "no upstream")
		}
	}

	output, err := RunGitPullWithLockRetry(ctx, t.Path, targetPullArgs(gitArgs), false, opts.IO)
	if err != nil {
		rebaseInitiatedHere := git.IsRebaseInProgress(ctx, t.Path)
		return attempt{
			result:         handleError(ctx, t, output, err, rebaseInitiatedHere),
			originalBranch: originalBranch,
			pulled:         true,
		}
	}

	outStr := strings.TrimSpace(string(output))
	result := Result{Target: *t, OriginalBranch: originalBranch}
	if strings.Contains(outStr, "Already up to date") {
		result.Outcome = OutcomeSkipped
		result.Status = "up-to-date"
	} else {
		result.Outcome = OutcomePulled
		result.Status = "done"
	}
	return attempt{result: result, originalBranch: originalBranch, pulled: true}
}

// targetPullArgs keeps every pull, not just the root's, out of nested
// submodules. RunAll already pulls each nested repo as its own target, and it
// may be running at the same moment, so a parent pull that recursed into it
// would put two git processes in one repository. User args come after, so an
// explicit --recurse-submodules still wins.
func targetPullArgs(gitArgs []string) []string {
	return append([]string{"--no-recurse-submodules"}, gitArgs...)
}

func skipped(t Target, status string) attempt {
	return attempt{result: Result{
		Target:  t,
		Outcome: OutcomeSkipped,
		Status:  status,
	}}
}

func checkoutDefaultIfNeeded(ctx context.Context, t *Target) (originalBranch string, switched bool, err error) {
	originalBranch = t.Branch

	if t.Branch != "" && t.Branch != "HEAD" {
		return originalBranch, false, nil
	}

	branch, err := git.CheckoutDefaultBranch(ctx, t.Path)
	if err != nil {
		return originalBranch, false, err
	}
	t.Branch = branch
	return originalBranch, true, nil
}

func handleError(ctx context.Context, t *Target, output []byte, err error, rebaseInitiatedHere bool) Result {
	if git.IsRebaseInProgress(ctx, t.Path) {
		if rebaseInitiatedHere {
			_ = abortRebase(ctx, t.Path)
			return Result{
				Target:       *t,
				Outcome:      OutcomeFailed,
				Status:       "conflict (aborted rebase)",
				ErrorMessage: fmt.Sprintf("  %s: rebase conflict (try: %s)", t.Name, PullNoRebaseHint(t)),
			}
		}

		return Result{
			Target:       *t,
			Outcome:      OutcomeFailed,
			Status:       "failed (pre-existing rebase in progress; not aborted)",
			ErrorMessage: fmt.Sprintf("  %s: pull failed; rebase in progress -- resolve it manually", t.Name),
		}
	}

	errMsg := strings.TrimSpace(string(output))
	if isDivergentError(errMsg) {
		errMsg = "branches diverged (try: camp pull all --ff-only, --rebase, or resolve manually)"
	} else if errMsg == "" {
		errMsg = err.Error()
	}
	return Result{
		Target:       *t,
		Outcome:      OutcomeFailed,
		Status:       "failed",
		ErrorMessage: fmt.Sprintf("  %s: %s", t.Name, errMsg),
	}
}

func discoverSubmodules(ctx context.Context, campRoot string, noRecurse bool) ([]string, error) {
	if noRecurse {
		paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to list submodules")
		}
		return paths, nil
	}
	paths, err := git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list submodules")
	}
	return paths, nil
}

func buildTargets(ctx context.Context, campRoot string, paths []string) []Target {
	targets := make([]Target, 0, len(paths)+1)
	rootBranch, _ := git.Output(ctx, campRoot, "rev-parse", "--abbrev-ref", "HEAD")
	targets = append(targets, Target{
		Name:   "camp root",
		Path:   campRoot,
		Branch: rootBranch,
		IsRoot: true,
	})
	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		branch, _ := git.Output(ctx, fullPath, "rev-parse", "--abbrev-ref", "HEAD")
		targets = append(targets, Target{
			Name:    git.SubmoduleDisplayName(p),
			Path:    fullPath,
			RelPath: p,
			Branch:  branch,
		})
	}
	return targets
}

// PullNoRebaseHint returns the retry hint for a failed rebase pull.
func PullNoRebaseHint(t *Target) string {
	if t != nil && !t.IsRoot && t.RelPath != "" {
		return fmt.Sprintf("camp pull --project=%s --no-rebase", t.RelPath)
	}
	return "camp pull --no-rebase"
}

// ChangedRefs returns submodule paths with new refs after pulling.
func ChangedRefs(ctx context.Context, campRoot string, subPaths []string) []string {
	var changed []string
	for _, p := range subPaths {
		fullPath := filepath.Join(campRoot, p)
		if git.HasPathDiff(ctx, campRoot, fullPath) {
			changed = append(changed, p)
		}
	}
	return changed
}

func isDivergentError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "divergent branches") ||
		strings.Contains(lower, "need to specify how to reconcile")
}

func abortRebase(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rebase", "--abort")
	return cmd.Run()
}

// DefaultIO returns streams matching normal command-line execution.
func DefaultIO() IO {
	return IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}
