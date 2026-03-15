//go:build windows

package git

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// LockRemovalFailure captures a stale lock cleanup failure with path context.
type LockRemovalFailure struct {
	Info LockInfo
	Err  error
}

// Unwrap returns the underlying error.
func (f *LockRemovalFailure) Unwrap() error {
	return f.Err
}

// RemovalResult summarizes the outcome of a lock removal operation.
type RemovalResult struct {
	Removed    []LockInfo
	Active     []LockInfo
	Failed     []LockRemovalFailure
	TotalLocks int
}

// Summary returns a human-readable summary.
func (r *RemovalResult) Summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Processed %d lock file(s)\n", r.TotalLocks)

	if len(r.Removed) > 0 {
		fmt.Fprintf(&sb, "  Removed (stale): %d\n", len(r.Removed))
		for _, info := range r.Removed {
			fmt.Fprintf(&sb, "    - %s\n", info.Path)
		}
	}

	if len(r.Active) > 0 {
		fmt.Fprintf(&sb, "  Active (waiting or blocked): %d\n", len(r.Active))
		for _, info := range r.Active {
			if info.ProcessID > 0 {
				fmt.Fprintf(&sb, "    - %s (PID %d: %s)\n",
					info.Path, info.ProcessID, info.Command)
			} else {
				fmt.Fprintf(&sb, "    - %s\n", info.Path)
			}
		}
	}

	if len(r.Failed) > 0 {
		fmt.Fprintf(&sb, "  Failed to remove (stale): %d\n", len(r.Failed))
		for _, failure := range r.Failed {
			fmt.Fprintf(&sb, "    - %s (%v)\n", failure.Info.Path, failure.Err)
		}
	}

	return sb.String()
}

// AllRemoved returns true if all locks were successfully removed.
func (r *RemovalResult) AllRemoved() bool {
	return len(r.Active) == 0 && len(r.Failed) == 0
}

// LockInfo contains information about a lock file.
type LockInfo struct {
	Path      string
	Stale     bool
	ProcessID int
	Command   string
}

// CheckLocksStaleness is a no-op on Windows. Lock staleness detection requires
// Unix-specific tools (fuser/lsof) that are not available on Windows.
// All locks are reported as active since staleness cannot be determined.
func CheckLocksStaleness(_ context.Context, locks []string) (stale, active []LockInfo, err error) {
	var activeLocks []LockInfo
	for _, l := range locks {
		activeLocks = append(activeLocks, LockInfo{Path: l, Stale: false})
	}
	return nil, activeLocks, nil
}

// RemoveStaleLocks is a no-op on Windows. Lock staleness detection requires
// Unix-specific tools (fuser/lsof) that are not available on Windows.
func RemoveStaleLocks(_ context.Context, _ []string, _ *slog.Logger) (removed, active []LockInfo, failed []LockRemovalFailure, err error) {
	return nil, nil, nil, nil
}

// CleanStaleLocks is a no-op on Windows. Lock staleness detection requires
// Unix-specific tools (fuser/lsof) that are not available on Windows.
func CleanStaleLocks(_ context.Context, _ string, _ *slog.Logger) (*RemovalResult, error) {
	return &RemovalResult{TotalLocks: 0}, nil
}

// WaitForLockRelease is a no-op on Windows. Returns nil immediately.
func WaitForLockRelease(_ context.Context, _ string, _ time.Duration, _ *slog.Logger) error {
	return nil
}
