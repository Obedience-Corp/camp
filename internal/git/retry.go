package git

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetryConfig configures lock-aware retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int
	// InitialBackoff is the delay before the first retry.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential backoff growth.
	MaxBackoff time.Duration
	// WaitForActive enables waiting for active locks to be released.
	WaitForActive bool
	// ActiveLockWait is how long to wait for active locks (if WaitForActive is true).
	ActiveLockWait time.Duration
	// Logger is the logger for retry operations (optional).
	Logger *slog.Logger
	// OperationName is used for log messages (e.g., "commit", "submodule add").
	OperationName string
}

// DefaultRetryConfig returns standard retry settings.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		WaitForActive:  false,
		ActiveLockWait: 5 * time.Second,
		Logger:         slog.Default(),
		OperationName:  "operation",
	}
}

// SubmoduleRetryConfig returns retry settings optimized for submodule operations.
// These operations are more sensitive to lock issues and benefit from waiting
// for active locks to be released.
func SubmoduleRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		WaitForActive:  true,
		ActiveLockWait: 5 * time.Second,
		Logger:         slog.Default(),
		OperationName:  "submodule",
	}
}

// WithLockRetry executes an operation with automatic lock handling.
// It retries on lock errors, cleans stale locks, and optionally waits for
// active locks to be released.
//
// The operation function should return a LockError when encountering git lock issues.
// Other errors are returned immediately without retry.
func WithLockRetry(ctx context.Context, repoPath string, cfg RetryConfig, operation func() error) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
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

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before each attempt
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := operation()
		if err == nil {
			return nil // Success
		}

		// Check if it's a lock error
		if !isLockError(err) {
			return err // Non-lock error, don't retry
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := CleanStaleLocks(ctx, repoPath, cfg.Logger)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks during %s retry (attempt %d): %w",
				cfg.OperationName, attempt, cleanErr)
		}

		// If we removed stale locks, retry after brief delay
		if len(result.Removed) > 0 {
			cfg.Logger.Info("retrying after stale lock cleanup",
				"operation", cfg.OperationName,
				"attempt", attempt,
				"removed", len(result.Removed))
			time.Sleep(backoff)
			backoff = min(backoff*2, cfg.MaxBackoff)
			continue
		}

		// If active locks found and we're configured to wait for them
		if len(result.Skipped) > 0 && cfg.WaitForActive {
			cfg.Logger.Info("waiting for active lock to release",
				"operation", cfg.OperationName,
				"attempt", attempt,
				"active_locks", len(result.Skipped))

			// Wait for the first active lock (usually there's only one)
			waitErr := WaitForLockRelease(ctx, result.Skipped[0].Path, cfg.ActiveLockWait, cfg.Logger)
			if waitErr == nil {
				// Lock released! Clean up any remaining stale locks and retry
				CleanStaleLocks(ctx, repoPath, cfg.Logger)
				continue
			}

			// Timeout waiting for lock - but still have attempts left
			if attempt < cfg.MaxAttempts {
				cfg.Logger.Warn("lock wait timeout, will retry",
					"operation", cfg.OperationName,
					"attempt", attempt,
					"pid", result.Skipped[0].ProcessID,
					"path", result.Skipped[0].Path)
				time.Sleep(backoff)
				backoff = min(backoff*2, cfg.MaxBackoff)
				continue
			}

			// Final attempt failed with active lock
			return fmt.Errorf("%s failed: lock held by active process (PID %d) after waiting: %w",
				cfg.OperationName, result.Skipped[0].ProcessID, lastErr)
		}

		// Active locks found but we're not waiting for them
		if len(result.Skipped) > 0 && !cfg.WaitForActive {
			// If we couldn't remove any locks and there are active ones, don't retry
			if len(result.Removed) == 0 {
				return fmt.Errorf("%s failed: lock held by active process: %w",
					cfg.OperationName, lastErr)
			}
		}

		// No locks found but still failed - apply backoff and retry
		cfg.Logger.Info("retrying operation",
			"operation", cfg.OperationName,
			"attempt", attempt)
		time.Sleep(backoff)
		backoff = min(backoff*2, cfg.MaxBackoff)
	}

	return fmt.Errorf("%s failed after %d attempts: %w", cfg.OperationName, cfg.MaxAttempts, lastErr)
}
