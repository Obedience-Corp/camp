//go:build unix

package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LockInfo contains information about a lock file.
type LockInfo struct {
	// Path is the path to the lock file.
	Path string
	// Stale is true if no process is holding the lock.
	Stale bool
	// ProcessID is the PID of the process holding the lock (0 if stale).
	ProcessID int
	// Command is the name of the command holding the lock.
	Command string
}

// IsLockStale checks if an index.lock file is from a dead process.
// Returns true if the lock is stale (safe to remove), false if active.
func IsLockStale(ctx context.Context, lockPath string) (bool, *LockInfo, error) {
	info := &LockInfo{Path: lockPath}

	// Try fuser first (more reliable)
	stale, pid, err := checkWithFuser(ctx, lockPath)
	if err == nil {
		info.Stale = stale
		info.ProcessID = pid
		return stale, info, nil
	}

	// Fall back to lsof
	stale, pid, cmd, err := checkWithLsof(ctx, lockPath)
	if err != nil {
		return false, info, fmt.Errorf("cannot determine lock status for %s: %w", lockPath, err)
	}

	info.Stale = stale
	info.ProcessID = pid
	info.Command = cmd
	return stale, info, nil
}

// checkWithFuser uses fuser to check if any process has the file open.
func checkWithFuser(ctx context.Context, lockPath string) (stale bool, pid int, err error) {
	cmd := exec.CommandContext(ctx, "fuser", lockPath)
	output, err := cmd.Output()

	if err != nil {
		// fuser exits non-zero if no process is using the file
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// No process using file = stale lock
				return true, 0, nil
			}
		}
		// Check if fuser is not installed
		if strings.Contains(err.Error(), "executable file not found") ||
			strings.Contains(err.Error(), "not found") {
			return false, 0, err // Fall back to lsof
		}
		return false, 0, err
	}

	// fuser outputs PIDs separated by spaces
	pidStr := strings.TrimSpace(string(output))
	if pidStr == "" {
		return true, 0, nil
	}

	// Parse first PID (remove any trailing letters like 'f' or 'c')
	pids := strings.Fields(pidStr)
	if len(pids) > 0 {
		// Remove any suffix letters from PID
		pidClean := strings.TrimRight(pids[0], "abcdefghijklmnopqrstuvwxyz")
		pid, _ = strconv.Atoi(pidClean)
	}

	return false, pid, nil
}

// checkWithLsof uses lsof as fallback to check file usage.
func checkWithLsof(ctx context.Context, lockPath string) (stale bool, pid int, cmd string, err error) {
	lsofCmd := exec.CommandContext(ctx, "lsof", lockPath)
	output, err := lsofCmd.Output()

	if err != nil {
		// lsof exits non-zero if file not open by any process
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true, 0, "", nil
			}
		}
		return false, 0, "", fmt.Errorf("lsof command failed: %w", err)
	}

	// Parse lsof output
	// Format: COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
	lines := strings.Split(string(output), "\n")
	for _, line := range lines[1:] { // Skip header
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			cmd = fields[0]
			pid, _ = strconv.Atoi(fields[1])
			return false, pid, cmd, nil
		}
	}

	// No process found in output
	return true, 0, "", nil
}

// CheckLocksStaleness checks multiple locks and categorizes them.
func CheckLocksStaleness(ctx context.Context, locks []string) (stale, active []LockInfo, err error) {
	for _, lockPath := range locks {
		if ctx.Err() != nil {
			return stale, active, ctx.Err()
		}

		isStale, info, err := IsLockStale(ctx, lockPath)
		if err != nil {
			// Can't determine status - treat as active for safety
			info = &LockInfo{Path: lockPath, Stale: false}
			active = append(active, *info)
			continue
		}

		if isStale {
			stale = append(stale, *info)
		} else {
			active = append(active, *info)
		}
	}

	return stale, active, nil
}

// removeSingleLock attempts to remove a single lock file after verifying it's stale.
// Returns the lock info with removal result.
func removeSingleLock(ctx context.Context, lockPath string, logger *slog.Logger) (*LockInfo, error) {
	// Step 1: Verify lock file still exists
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return &LockInfo{
			Path:  lockPath,
			Stale: true,
		}, nil // Already gone, success
	}

	// Step 2: Double-check staleness (critical for safety)
	stale, info, err := IsLockStale(ctx, lockPath)
	if err != nil {
		return info, fmt.Errorf("cannot verify lock status before removal for %s: %w", lockPath, err)
	}

	if !stale {
		logger.Warn("lock is active, cannot remove",
			"path", lockPath,
			"pid", info.ProcessID,
			"command", info.Command)
		return info, ErrLockActive
	}

	// Step 3: Remove the lock file
	logger.Info("removing stale lock", "path", lockPath)
	if err := os.Remove(lockPath); err != nil {
		return info, fmt.Errorf("failed to remove lock file %s: %w", lockPath, err)
	}

	info.Stale = true
	return info, nil
}

// RemoveStaleLocks attempts to remove all stale lock files from the provided list.
// Returns lists of successfully removed locks, skipped locks (active), and any errors.
func RemoveStaleLocks(ctx context.Context, locks []string, logger *slog.Logger) (removed, skipped []LockInfo, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	for _, lockPath := range locks {
		// Check context for cancellation
		if ctx.Err() != nil {
			return removed, skipped, ctx.Err()
		}

		info, removeErr := removeSingleLock(ctx, lockPath, logger)
		if removeErr != nil {
			if errors.Is(removeErr, ErrLockActive) {
				// Active lock - skip but don't fail
				skipped = append(skipped, *info)
				continue
			}
			// Other error - log and continue
			logger.Error("failed to remove lock",
				"path", lockPath,
				"error", removeErr)
			if info != nil {
				skipped = append(skipped, *info)
			} else {
				skipped = append(skipped, LockInfo{Path: lockPath})
			}
			continue
		}

		removed = append(removed, *info)
	}

	return removed, skipped, nil
}

// RemovalResult summarizes the outcome of a lock removal operation.
type RemovalResult struct {
	Removed    []LockInfo
	Skipped    []LockInfo
	TotalLocks int
}

// Summary returns a human-readable summary.
func (r *RemovalResult) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Processed %d lock file(s)\n", r.TotalLocks))

	if len(r.Removed) > 0 {
		sb.WriteString(fmt.Sprintf("  Removed (stale): %d\n", len(r.Removed)))
		for _, info := range r.Removed {
			sb.WriteString(fmt.Sprintf("    - %s\n", info.Path))
		}
	}

	if len(r.Skipped) > 0 {
		sb.WriteString(fmt.Sprintf("  Skipped (active or error): %d\n", len(r.Skipped)))
		for _, info := range r.Skipped {
			if info.ProcessID > 0 {
				sb.WriteString(fmt.Sprintf("    - %s (PID %d: %s)\n",
					info.Path, info.ProcessID, info.Command))
			} else {
				sb.WriteString(fmt.Sprintf("    - %s\n", info.Path))
			}
		}
	}

	return sb.String()
}

// AllRemoved returns true if all locks were successfully removed.
func (r *RemovalResult) AllRemoved() bool {
	return len(r.Skipped) == 0
}

// CleanStaleLocks is a convenience function that finds and removes all stale locks
// in a git repository.
func CleanStaleLocks(ctx context.Context, repoRoot string, logger *slog.Logger) (*RemovalResult, error) {
	// Find all locks
	locks, err := FindLocksInRepository(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to find locks: %w", err)
	}

	if len(locks) == 0 {
		return &RemovalResult{TotalLocks: 0}, nil
	}

	// Remove stale ones
	removed, skipped, err := RemoveStaleLocks(ctx, locks, logger)
	if err != nil {
		return nil, err
	}

	return &RemovalResult{
		Removed:    removed,
		Skipped:    skipped,
		TotalLocks: len(locks),
	}, nil
}

// WaitForLockRelease waits for an active lock to be released.
// It polls the lock status with exponential backoff until the lock is released
// or the timeout is exceeded.
// Returns nil if the lock was released, ErrLockTimeout if timeout exceeded.
func WaitForLockRelease(ctx context.Context, lockPath string, timeout time.Duration, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 100 * time.Millisecond
	maxPollInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if lock still exists
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			logger.Info("lock released (file removed)", "path", lockPath)
			return nil
		}

		// Check if lock is now stale
		stale, info, err := IsLockStale(ctx, lockPath)
		if err == nil && stale {
			logger.Info("lock became stale", "path", lockPath)
			return nil
		}

		// Log waiting status
		remaining := time.Until(deadline).Round(100 * time.Millisecond)
		if info != nil && info.ProcessID > 0 {
			logger.Debug("waiting for lock release",
				"path", lockPath,
				"pid", info.ProcessID,
				"remaining", remaining)
		} else {
			logger.Debug("waiting for lock release",
				"path", lockPath,
				"remaining", remaining)
		}

		// Wait before next poll
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}

		// Exponential backoff for polling (capped)
		pollInterval = min(pollInterval*2, maxPollInterval)
	}

	return ErrLockTimeout
}
