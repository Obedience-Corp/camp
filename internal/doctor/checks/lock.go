package checks

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Obedience-Corp/camp/internal/doctor"
	"github.com/Obedience-Corp/camp/internal/git"
)

// LockCheck detects stale git index.lock files in the campaign and submodules.
type LockCheck struct{}

// NewLockCheck creates a new git lock file check.
func NewLockCheck() *LockCheck {
	return &LockCheck{}
}

// ID returns the check identifier.
func (c *LockCheck) ID() string {
	return "lock"
}

// Name returns the human-readable check name.
func (c *LockCheck) Name() string {
	return "Git Lock Files"
}

// Description returns a brief explanation of what this check does.
func (c *LockCheck) Description() string {
	return "Detects stale index.lock files in the campaign and submodules"
}

// Run performs the lock file check.
func (c *LockCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Total:   0,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	// Find all lock files in the repository
	locks, err := git.FindLocksInRepository(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("find lock files: %w", err)
	}

	result.Total = len(locks)

	if len(locks) == 0 {
		return result, nil
	}

	// Check staleness of each lock
	stale, active, err := git.CheckLocksStaleness(ctx, locks)
	if err != nil {
		return nil, fmt.Errorf("check lock staleness: %w", err)
	}

	// Report stale locks as errors (auto-fixable)
	for _, info := range stale {
		result.Passed = false
		result.Issues = append(result.Issues, doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Description: fmt.Sprintf("Stale lock file: %s", info.Path),
			FixCommand:  fmt.Sprintf("rm %s", info.Path),
			AutoFixable: true,
			Details: map[string]any{
				"path": info.Path,
				"type": "stale_lock",
			},
		})
	}

	// Report active locks as warnings (not auto-fixable)
	for _, info := range active {
		details := map[string]any{
			"path": info.Path,
			"type": "active_lock",
		}
		desc := fmt.Sprintf("Active lock file: %s", info.Path)

		if info.ProcessID > 0 {
			details["pid"] = info.ProcessID
			details["command"] = info.Command
			desc = fmt.Sprintf("Active lock file: %s (PID %d: %s)", info.Path, info.ProcessID, info.Command)
		}

		result.Issues = append(result.Issues, doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Description: desc,
			AutoFixable: false,
			Details:     details,
		})
	}

	return result, nil
}

// Fix attempts to remove stale lock files.
func (c *LockCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(issues) == 0 {
		return nil, nil
	}

	// Collect stale lock paths from auto-fixable issues
	var stalePaths []string
	for _, issue := range issues {
		if !issue.AutoFixable {
			continue
		}
		if path, ok := issue.Details["path"].(string); ok {
			stalePaths = append(stalePaths, path)
		}
	}

	if len(stalePaths) == 0 {
		return nil, nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	removed, _, _, err := git.RemoveStaleLocks(ctx, stalePaths, logger)
	if err != nil {
		return nil, fmt.Errorf("remove stale locks: %w", err)
	}

	// Map removed paths back to their issues
	removedSet := make(map[string]bool)
	for _, info := range removed {
		removedSet[info.Path] = true
	}

	var fixed []doctor.Issue
	for _, issue := range issues {
		if path, ok := issue.Details["path"].(string); ok && removedSet[path] {
			fixed = append(fixed, issue)
		}
	}

	return fixed, nil
}
