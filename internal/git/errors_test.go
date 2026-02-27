package git

import (
	"errors"
	"testing"
)

func TestGitErrorType_String(t *testing.T) {
	tests := []struct {
		errType GitErrorType
		want    string
	}{
		{GitErrorUnknown, "unknown"},
		{GitErrorLock, "lock"},
		{GitErrorNoChanges, "no_changes"},
		{GitErrorNotRepo, "not_repo"},
		{GitErrorPermission, "permission"},
		{GitErrorNetwork, "network"},
		{GitErrorSubmodule, "submodule"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.errType.String()
			if got != tt.want {
				t.Errorf("GitErrorType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyGitError(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		exitCode int
		want     GitErrorType
	}{
		{
			name:     "lock error",
			stderr:   "fatal: Unable to create '/repo/.git/index.lock': File exists.",
			exitCode: 128,
			want:     GitErrorLock,
		},
		{
			name:     "nothing to commit",
			stderr:   "nothing to commit, working tree clean",
			exitCode: 1,
			want:     GitErrorNoChanges,
		},
		{
			name:     "not a git repository",
			stderr:   "fatal: not a git repository (or any of the parent directories): .git",
			exitCode: 128,
			want:     GitErrorNotRepo,
		},
		{
			name:     "permission denied",
			stderr:   "error: could not lock config file: Permission denied",
			exitCode: 128,
			want:     GitErrorPermission,
		},
		{
			name:     "network error - host resolution",
			stderr:   "fatal: could not resolve host: github.com",
			exitCode: 128,
			want:     GitErrorNetwork,
		},
		{
			name:     "network error - connection refused",
			stderr:   "fatal: unable to access: connection refused",
			exitCode: 128,
			want:     GitErrorNetwork,
		},
		{
			name:     "network error - timeout",
			stderr:   "fatal: connection timed out",
			exitCode: 128,
			want:     GitErrorNetwork,
		},
		{
			name:     "submodule error",
			stderr:   "fatal: Submodule 'lib' could not be updated",
			exitCode: 128,
			want:     GitErrorSubmodule,
		},
		{
			name:     "unknown error",
			stderr:   "fatal: some unknown git error occurred",
			exitCode: 1,
			want:     GitErrorUnknown,
		},
		{
			name:     "case insensitive - lock",
			stderr:   "FATAL: Unable to create INDEX.LOCK",
			exitCode: 128,
			want:     GitErrorLock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGitError(tt.stderr, tt.exitCode)
			if got != tt.want {
				t.Errorf("ClassifyGitError() = %v (%s), want %v (%s)",
					got, got.String(), tt.want, tt.want.String())
			}
		})
	}
}

func TestLockError_Error(t *testing.T) {
	tests := []struct {
		name    string
		lockErr *LockError
		wantMsg string
	}{
		{
			name: "stale lock",
			lockErr: &LockError{
				Path:  "/repo/.git/index.lock",
				Stale: true,
			},
			wantMsg: "stale lock at /repo/.git/index.lock",
		},
		{
			name: "active lock with PID",
			lockErr: &LockError{
				Path:      "/repo/.git/index.lock",
				ProcessID: 12345,
				Stale:     false,
			},
			wantMsg: "lock at /repo/.git/index.lock held by process 12345",
		},
		{
			name: "lock without PID",
			lockErr: &LockError{
				Path: "/repo/.git/index.lock",
			},
			wantMsg: "lock file exists at /repo/.git/index.lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.lockErr.Error()
			if got != tt.wantMsg {
				t.Errorf("LockError.Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestLockError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	lockErr := &LockError{
		Path: "/repo/.git/index.lock",
		Err:  underlying,
	}

	if !errors.Is(lockErr, underlying) {
		t.Error("LockError.Unwrap() should allow errors.Is to find underlying error")
	}
}

func TestNewLockError(t *testing.T) {
	path := "/repo/.git/index.lock"
	underlying := errors.New("test error")

	lockErr := NewLockError(path, underlying)

	if lockErr.Path != path {
		t.Errorf("NewLockError().Path = %q, want %q", lockErr.Path, path)
	}
	if lockErr.Err != underlying {
		t.Error("NewLockError().Err should be the underlying error")
	}
	if lockErr.Stale {
		t.Error("NewLockError() should not be stale by default")
	}
	if lockErr.ProcessID != 0 {
		t.Errorf("NewLockError().ProcessID = %d, want 0", lockErr.ProcessID)
	}
}

func TestNewStaleLockError(t *testing.T) {
	path := "/repo/.git/index.lock"

	lockErr := NewStaleLockError(path)

	if lockErr.Path != path {
		t.Errorf("NewStaleLockError().Path = %q, want %q", lockErr.Path, path)
	}
	if !lockErr.Stale {
		t.Error("NewStaleLockError() should be stale")
	}
}

func TestNewActiveLockError(t *testing.T) {
	path := "/repo/.git/index.lock"
	pid := 12345

	lockErr := NewActiveLockError(path, pid)

	if lockErr.Path != path {
		t.Errorf("NewActiveLockError().Path = %q, want %q", lockErr.Path, path)
	}
	if lockErr.ProcessID != pid {
		t.Errorf("NewActiveLockError().ProcessID = %d, want %d", lockErr.ProcessID, pid)
	}
	if lockErr.Stale {
		t.Error("NewActiveLockError() should not be stale")
	}
	if !errors.Is(lockErr, ErrLockActive) {
		t.Error("NewActiveLockError() should wrap ErrLockActive")
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are distinct
	sentinels := []error{
		ErrLockActive,
		ErrLockRemovalFailed,
		ErrNotRepository,
		ErrNoChanges,
		ErrStage,
		ErrCommitFailed,
		ErrCommitCancelled,
		ErrCommitOptionsRequired,
		ErrCommitMessageRequired,
		ErrNoFilesSpecified,
	}

	for i, err1 := range sentinels {
		for j, err2 := range sentinels {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("sentinel errors should be distinct: %v == %v", err1, err2)
			}
		}
	}
}

func TestGitOpError_Error(t *testing.T) {
	tests := []struct {
		name    string
		opErr   *GitOpError
		wantMsg string
	}{
		{
			name: "with detail",
			opErr: &GitOpError{
				Op:      "commit",
				ErrType: GitErrorUnknown,
				Detail:  "some git output",
				Cause:   errors.New("exit status 1"),
			},
			wantMsg: "git commit failed (unknown): some git output",
		},
		{
			name: "without detail",
			opErr: &GitOpError{
				Op:      "add",
				ErrType: GitErrorPermission,
				Cause:   errors.New("exit status 128"),
			},
			wantMsg: "git add failed (permission)",
		},
		{
			name: "lock type with detail",
			opErr: &GitOpError{
				Op:      "diff --cached",
				ErrType: GitErrorLock,
				Detail:  "index.lock exists",
				Cause:   errors.New("exit status 128"),
			},
			wantMsg: "git diff --cached failed (lock): index.lock exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opErr.Error()
			if got != tt.wantMsg {
				t.Errorf("GitOpError.Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestGitOpError_Unwrap(t *testing.T) {
	underlying := errors.New("exit status 1")
	opErr := &GitOpError{
		Op:    "commit",
		Cause: underlying,
	}

	if !errors.Is(opErr, underlying) {
		t.Error("GitOpError.Unwrap() should allow errors.Is to find underlying error")
	}
}

func TestGitOpError_As(t *testing.T) {
	opErr := &GitOpError{
		Op:      "add",
		ErrType: GitErrorPermission,
		Detail:  "permission denied",
		Cause:   errors.New("exit status 128"),
	}

	var target *GitOpError
	if !errors.As(opErr, &target) {
		t.Fatal("errors.As should match *GitOpError")
	}
	if target.Op != "add" {
		t.Errorf("GitOpError.Op = %q, want %q", target.Op, "add")
	}
	if target.ErrType != GitErrorPermission {
		t.Errorf("GitOpError.ErrType = %v, want %v", target.ErrType, GitErrorPermission)
	}
}
