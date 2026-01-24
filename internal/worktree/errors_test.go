package worktree

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWorktreeError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "project not found",
			err:      ProjectNotFound("my-api"),
			contains: "project=my-api",
		},
		{
			name:     "worktree exists",
			err:      WorktreeAlreadyExists("my-api", "feature"),
			contains: "worktree=feature",
		},
		{
			name:     "worktree not found",
			err:      WorktreeNotFoundError("my-api", "old-feature"),
			contains: "worktree=old-feature",
		},
		{
			name:     "branch not found",
			err:      BranchNotFoundError("my-api", "feature/xyz"),
			contains: "project=my-api",
		},
		{
			name:     "invalid name",
			err:      InvalidWorktreeName("bad/name", "contains slash"),
			contains: "contains slash",
		},
		{
			name:     "git failed with cause",
			err:      GitOperationFailed("my-api", "worktree add", fmt.Errorf("exit status 1")),
			contains: "exit status 1",
		},
		{
			name:     "stale worktree",
			err:      StaleWorktreeError("my-api", "old-feature", "branch deleted"),
			contains: "branch deleted",
		},
		{
			name:     "removal failed",
			err:      RemovalFailed("my-api", "broken", fmt.Errorf("permission denied")),
			contains: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("error %q should contain %q", tt.err, tt.contains)
			}
		})
	}
}

func TestWorktreeError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := NewError(ErrCodeGitFailed).WithCause(cause)

	if unwrapped := errors.Unwrap(err); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestWorktreeError_SentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
	}{
		{
			name:     "project not found wraps sentinel",
			err:      ProjectNotFound("test"),
			sentinel: ErrProjectNotFound,
		},
		{
			name:     "worktree exists wraps sentinel",
			err:      WorktreeAlreadyExists("test", "wt"),
			sentinel: ErrWorktreeExists,
		},
		{
			name:     "worktree not found wraps sentinel",
			err:      WorktreeNotFoundError("test", "wt"),
			sentinel: ErrWorktreeNotFound,
		},
		{
			name:     "branch not found wraps sentinel",
			err:      BranchNotFoundError("test", "main"),
			sentinel: ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false, want true", tt.err, tt.sentinel)
			}
		})
	}
}

func TestWorktreeError_Builder(t *testing.T) {
	err := NewError(ErrCodeWorktreeNotFound).
		WithProject("my-project").
		WithWorktree("feature-branch").
		WithBranch("feature/123").
		WithPath("/some/path")

	msg := err.Error()

	if !strings.Contains(msg, "project=my-project") {
		t.Error("Error message should contain project")
	}
	if !strings.Contains(msg, "worktree=feature-branch") {
		t.Error("Error message should contain worktree")
	}
	if !strings.Contains(msg, ErrCodeWorktreeNotFound) {
		t.Error("Error message should contain error code")
	}

	// Branch and path are stored but not shown in Error() by default
	if err.Branch != "feature/123" {
		t.Error("Branch should be stored")
	}
	if err.Path != "/some/path" {
		t.Error("Path should be stored")
	}
}
