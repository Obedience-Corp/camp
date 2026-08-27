package git

import (
	"context"
	"errors"
	"log/slog"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// RetryConfig configures lock-aware retry behavior using a cycle-based approach.
// Within each cycle, fast retries execute immediately with no delay.
// Between cycles, stale locks are cleaned and a backoff delay is applied.
type RetryConfig struct {
	// AttemptsPerCycle is the number of fast retries per cycle (no delay between them).
	AttemptsPerCycle int
	// MaxCycles is the number of cycles before giving up.
	MaxCycles int
	// InitialBackoff is the delay between cycles (not between individual attempts).
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential backoff growth between cycles.
	MaxBackoff time.Duration
	// WaitForActive enables waiting for active locks to be released between cycles.
	WaitForActive bool
	// ActiveLockWait is how long to wait for active locks (if WaitForActive is true).
	ActiveLockWait time.Duration
	// MaxActiveLockWaits is how many ActiveLockWait timeouts to absorb before
	// giving up. Zero fails on the first one, which is what a user waiting in
	// front of a terminal wants: a lock that has not cleared in ActiveLockWait
	// is worth reporting rather than waiting on again. A background worker has
	// nobody waiting and sets this so a busy repository costs patience rather
	// than a dropped job.
	MaxActiveLockWaits int
	// Logger is the logger for retry operations (optional).
	Logger *slog.Logger
	// OperationName is used for log messages (e.g., "commit", "submodule add").
	OperationName string
}

// DefaultRetryConfig returns standard retry settings.
// WaitForActive defaults to true; callers that need fail-fast should override.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		AttemptsPerCycle: 3,
		MaxCycles:        2,
		InitialBackoff:   200 * time.Millisecond,
		MaxBackoff:       2 * time.Second,
		WaitForActive:    true,
		ActiveLockWait:   5 * time.Second,
		Logger:           slog.Default(),
		OperationName:    "operation",
	}
}

// SubmoduleRetryConfig returns retry settings optimized for submodule operations.
// These operations are more sensitive to lock issues and benefit from waiting
// for active locks to be released.
func SubmoduleRetryConfig() RetryConfig {
	return RetryConfig{
		AttemptsPerCycle: 3,
		MaxCycles:        2,
		InitialBackoff:   200 * time.Millisecond,
		MaxBackoff:       2 * time.Second,
		WaitForActive:    true,
		ActiveLockWait:   5 * time.Second,
		Logger:           slog.Default(),
		OperationName:    "submodule",
	}
}

// BackgroundRetryConfig returns retry settings for work with no user waiting
// on it — the detached deferred-commit worker.
//
// The interactive profile is tuned for a person watching a prompt: six
// attempts across two cycles and one five-second active-lock wait, then report
// and let them decide. That budget is spent in about ten seconds, and it is
// too thin for a campaign root several agent sessions commit into at once.
// Every one of those sessions holds index.lock for a few milliseconds at a
// time, so the worker loses a race it would have won a moment later and parks
// a commit it had already been told to make. Execution failures do not spend
// the reclaim budget, so one lost race is a job in failed/, not a retry.
//
// The numbers, and what bounds them: twenty-four attempts over eight cycles
// (about ten seconds of backoff in total), a fifteen-second active-lock wait,
// and three such timeouts absorbed before the operation reports. Worst case is
// therefore a little over a minute — well inside jobs.drainMaxWait, so a drain
// still gives up after the worker rather than before it, and far short of the
// writer timeout a job may already be paying.
func BackgroundRetryConfig() RetryConfig {
	return RetryConfig{
		AttemptsPerCycle:   3,
		MaxCycles:          8,
		InitialBackoff:     250 * time.Millisecond,
		MaxBackoff:         2 * time.Second,
		WaitForActive:      true,
		ActiveLockWait:     15 * time.Second,
		MaxActiveLockWaits: 3,
		Logger:             slog.Default(),
		OperationName:      "operation",
	}
}

// WithLockRetry executes an operation with cycle-based lock handling.
//
// Within each cycle, fast retries execute immediately with no delay — this handles
// transient locks that clear on their own. Between cycles, stale locks are actively
// cleaned and a backoff delay is applied.
//
// The operation function should return a LockError when encountering git lock issues.
// Other errors are returned immediately without retry.
func WithLockRetry(ctx context.Context, repoPath string, cfg RetryConfig, operation func() error) error {
	if cfg.AttemptsPerCycle <= 0 {
		cfg.AttemptsPerCycle = 3
	}
	if cfg.MaxCycles <= 0 {
		cfg.MaxCycles = 2
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 200 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 2 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.OperationName == "" {
		cfg.OperationName = "operation"
	}

	var lastErr error
	backoff := cfg.InitialBackoff
	activeWaits := 0

	for cycle := 1; cycle <= cfg.MaxCycles; cycle++ {
		// Fast retry loop — no delays, no lock cleanup
		for attempt := 1; attempt <= cfg.AttemptsPerCycle; attempt++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			err := operation()
			if err == nil {
				return nil
			}

			if !isLockError(err) {
				return err
			}

			lastErr = err
		}

		// All fast attempts in this cycle failed with lock errors.
		// Actively intervene: clean stale locks.
		result, cleanErr := CleanStaleLocks(ctx, repoPath, cfg.Logger)
		if cleanErr != nil {
			return camperrors.Wrapf(cleanErr, "failed to clean locks during %s (cycle %d)",
				cfg.OperationName, cycle)
		}

		cfg.Logger.Debug("cycle completed, cleaning locks",
			"operation", cfg.OperationName,
			"cycle", cycle,
			"removed", len(result.Removed),
			"active", len(result.Active),
			"failed", len(result.Failed))

		if len(result.Failed) > 0 {
			first := result.Failed[0]
			return camperrors.Wrapf(first.Err, "%s failed while removing stale lock %s",
				cfg.OperationName, first.Info.Path)
		}

		// If active locks found and configured to wait
		if len(result.Active) > 0 && cfg.WaitForActive {
			waitErr := WaitForLockRelease(ctx, result.Active[0].Path, cfg.ActiveLockWait, cfg.Logger)
			if waitErr == nil {
				CleanStaleLocks(ctx, repoPath, cfg.Logger)
				continue // Start next cycle immediately
			}
			// An active-lock timeout is terminal by default: MaxCycles does not
			// apply, because a user waiting on the operation has already waited
			// ActiveLockWait for an answer. A caller that budgeted for more of
			// them absorbs this one and takes another cycle. Only a timeout is
			// absorbed — a cancelled wait is the process being told to stop.
			activeWaits++
			if !errors.Is(waitErr, ErrLockTimeout) || activeWaits > cfg.MaxActiveLockWaits {
				return camperrors.WrapJoinf(ErrLockActive, waitErr, "%s failed: active lock persisted at %s",
					cfg.OperationName, result.Active[0].Path)
			}
			cfg.Logger.Debug("active lock persisted, waiting again",
				"operation", cfg.OperationName,
				"cycle", cycle,
				"path", result.Active[0].Path,
				"waits", activeWaits,
				"budget", cfg.MaxActiveLockWaits)
			continue
		}

		// Active locks found but not waiting — fail fast
		if len(result.Active) > 0 && !cfg.WaitForActive && len(result.Removed) == 0 {
			return camperrors.Wrapf(lastErr, "%s failed: lock held by active process",
				cfg.OperationName)
		}

		// Apply backoff between cycles
		if cycle < cfg.MaxCycles {
			cfg.Logger.Debug("waiting before next cycle",
				"operation", cfg.OperationName,
				"cycle", cycle,
				"backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, cfg.MaxBackoff)
		}
	}

	totalAttempts := cfg.MaxCycles * cfg.AttemptsPerCycle
	return camperrors.Wrapf(lastErr, "%s failed after %d cycles (%d attempts)",
		cfg.OperationName, cfg.MaxCycles, totalAttempts)
}
