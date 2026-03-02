package errors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ValidationError
		wantMsg  string
		wantWrap error
	}{
		{
			name:     "with underlying error",
			err:      NewValidation("name", "must not be empty", io.EOF),
			wantMsg:  "validation error on name: must not be empty: EOF",
			wantWrap: io.EOF,
		},
		{
			name:     "without underlying error",
			err:      NewValidation("age", "must be positive", nil),
			wantMsg:  "validation error on age: must be positive",
			wantWrap: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := tt.err.Unwrap(); got != tt.wantWrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      *NotFoundError
		wantMsg  string
		wantWrap error
	}{
		{
			name:     "with underlying error",
			err:      NewNotFound("project", "my-proj", io.EOF),
			wantMsg:  "project not found: my-proj: EOF",
			wantWrap: io.EOF,
		},
		{
			name:     "without underlying error",
			err:      NewNotFound("campaign", "abc-123", nil),
			wantMsg:  "campaign not found: abc-123",
			wantWrap: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := tt.err.Unwrap(); got != tt.wantWrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestConfigError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ConfigError
		wantMsg  string
		wantWrap error
	}{
		{
			name:     "with underlying error",
			err:      NewConfig("database.url", "invalid format", io.EOF),
			wantMsg:  "config error (database.url): invalid format: EOF",
			wantWrap: io.EOF,
		},
		{
			name:     "without underlying error",
			err:      NewConfig("port", "must be between 1-65535", nil),
			wantMsg:  "config error (port): must be between 1-65535",
			wantWrap: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := tt.err.Unwrap(); got != tt.wantWrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestIOError(t *testing.T) {
	tests := []struct {
		name     string
		err      *IOError
		wantMsg  string
		wantWrap error
	}{
		{
			name:     "with underlying error",
			err:      NewIO("read", "/tmp/file.txt", io.EOF),
			wantMsg:  "io read failed on /tmp/file.txt: EOF",
			wantWrap: io.EOF,
		},
		{
			name:     "without underlying error",
			err:      NewIO("write", "/dev/null", nil),
			wantMsg:  "io write failed on /dev/null",
			wantWrap: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := tt.err.Unwrap(); got != tt.wantWrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestPermissionError(t *testing.T) {
	tests := []struct {
		name     string
		err      *PermissionError
		wantMsg  string
		wantWrap error
	}{
		{
			name:     "with underlying error",
			err:      NewPermission("write", "/etc/config", io.EOF),
			wantMsg:  "permission denied: write on /etc/config: EOF",
			wantWrap: io.EOF,
		},
		{
			name:     "without underlying error",
			err:      NewPermission("delete", "admin-resource", nil),
			wantMsg:  "permission denied: delete on admin-resource",
			wantWrap: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			if got := tt.err.Unwrap(); got != tt.wantWrap {
				t.Errorf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	wrapped := Wrap(ErrNotFound, "loading project")
	if !Is(wrapped, ErrNotFound) {
		t.Error("Is() should match wrapped sentinel error")
	}
	if Is(wrapped, ErrPermission) {
		t.Error("Is() should not match unrelated sentinel error")
	}
}

func TestErrorsAs(t *testing.T) {
	original := NewValidation("name", "required", nil)
	wrapped := fmt.Errorf("outer: %w", original)

	var ve *ValidationError
	if !As(wrapped, &ve) {
		t.Fatal("As() should extract ValidationError from wrapped error")
	}
	if ve.Field != "name" {
		t.Errorf("Field = %q, want %q", ve.Field, "name")
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		msg     string
		wantNil bool
		wantMsg string
	}{
		{
			name:    "nil error",
			err:     nil,
			msg:     "context",
			wantNil: true,
		},
		{
			name:    "wraps error",
			err:     io.EOF,
			msg:     "reading config",
			wantMsg: "reading config: EOF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrap(tt.err, tt.msg)
			if tt.wantNil {
				if got != nil {
					t.Errorf("Wrap(nil) = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("Wrap() returned nil for non-nil error")
			}
			if got.Error() != tt.wantMsg {
				t.Errorf("Wrap().Error() = %q, want %q", got.Error(), tt.wantMsg)
			}
		})
	}
}

func TestWrapf(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		format  string
		args    []any
		wantNil bool
		wantMsg string
	}{
		{
			name:    "nil error",
			err:     nil,
			format:  "op %s",
			args:    []any{"read"},
			wantNil: true,
		},
		{
			name:    "wraps error with format",
			err:     io.EOF,
			format:  "reading file %s",
			args:    []any{"/tmp/test"},
			wantMsg: "reading file /tmp/test: EOF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrapf(tt.err, tt.format, tt.args...)
			if tt.wantNil {
				if got != nil {
					t.Errorf("Wrapf(nil) = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("Wrapf() returned nil for non-nil error")
			}
			if got.Error() != tt.wantMsg {
				t.Errorf("Wrapf().Error() = %q, want %q", got.Error(), tt.wantMsg)
			}
		})
	}
}

func TestWrapJoin(t *testing.T) {
	sentinel := New("sentinel")
	cause := New("cause")

	t.Run("both errors with message", func(t *testing.T) {
		err := WrapJoin(sentinel, cause, "context")
		if err == nil {
			t.Fatal("WrapJoin returned nil")
		}
		if !Is(err, sentinel) {
			t.Error("errors.Is(err, sentinel) = false, want true")
		}
		if !Is(err, cause) {
			t.Error("errors.Is(err, cause) = false, want true")
		}
		if !strings.Contains(err.Error(), "context") {
			t.Errorf("error message missing context: %q", err.Error())
		}
	})

	t.Run("both errors without message", func(t *testing.T) {
		err := WrapJoin(sentinel, cause, "")
		if !Is(err, sentinel) {
			t.Error("errors.Is(err, sentinel) = false, want true")
		}
		if !Is(err, cause) {
			t.Error("errors.Is(err, cause) = false, want true")
		}
	})

	t.Run("nil sentinel", func(t *testing.T) {
		err := WrapJoin(nil, cause, "msg")
		if err == nil {
			t.Fatal("WrapJoin(nil, cause) returned nil")
		}
		if !Is(err, cause) {
			t.Error("errors.Is(err, cause) = false, want true")
		}
	})

	t.Run("nil cause", func(t *testing.T) {
		err := WrapJoin(sentinel, nil, "msg")
		if err == nil {
			t.Fatal("WrapJoin(sentinel, nil) returned nil")
		}
		if !Is(err, sentinel) {
			t.Error("errors.Is(err, sentinel) = false, want true")
		}
	})

	t.Run("both nil", func(t *testing.T) {
		err := WrapJoin(nil, nil, "msg")
		if err != nil {
			t.Errorf("WrapJoin(nil, nil) = %v, want nil", err)
		}
	})
}

func TestWrapJoinf(t *testing.T) {
	sentinel := New("sentinel")
	cause := New("cause")

	err := WrapJoinf(sentinel, cause, "op %s", "test")
	if err == nil {
		t.Fatal("WrapJoinf returned nil")
	}
	if !Is(err, sentinel) {
		t.Error("errors.Is(err, sentinel) = false, want true")
	}
	if !Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
	if !strings.Contains(err.Error(), "op test") {
		t.Errorf("error message missing formatted context: %q", err.Error())
	}
}

func TestContextCancellation(t *testing.T) {
	t.Run("wrap context.Canceled", func(t *testing.T) {
		err := Wrap(context.Canceled, "fetching data")
		if !Is(err, context.Canceled) {
			t.Error("wrapped context.Canceled should match with Is()")
		}
	})

	t.Run("wrap context.DeadlineExceeded", func(t *testing.T) {
		err := Wrap(context.DeadlineExceeded, "waiting for response")
		if !Is(err, context.DeadlineExceeded) {
			t.Error("wrapped context.DeadlineExceeded should match with Is()")
		}
	})

	t.Run("typed error wrapping context.Canceled", func(t *testing.T) {
		ioErr := NewIO("read", "/tmp/data", context.Canceled)
		if !Is(ioErr, context.Canceled) {
			t.Error("IOError wrapping context.Canceled should match with Is()")
		}
	})

	t.Run("typed error wrapping context.DeadlineExceeded", func(t *testing.T) {
		nf := NewNotFound("resource", "x", context.DeadlineExceeded)
		if !Is(nf, context.DeadlineExceeded) {
			t.Error("NotFoundError wrapping DeadlineExceeded should match with Is()")
		}
	})
}

func TestUnwrapAndNew(t *testing.T) {
	original := New("base error")
	wrapped := Wrap(original, "layer")
	unwrapped := Unwrap(wrapped)
	if unwrapped == nil {
		t.Fatal("Unwrap() should return the inner error")
	}
	if unwrapped.Error() != "base error" {
		t.Errorf("Unwrap().Error() = %q, want %q", unwrapped.Error(), "base error")
	}
}

func TestJoin(t *testing.T) {
	err1 := New("first")
	err2 := New("second")
	joined := Join(err1, err2)
	if joined == nil {
		t.Fatal("Join() should return non-nil for non-nil errors")
	}
	if !Is(joined, err1) {
		t.Error("joined error should match first error")
	}
	if !Is(joined, err2) {
		t.Error("joined error should match second error")
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []struct {
		err  error
		text string
	}{
		{ErrNotFound, "not found"},
		{ErrAlreadyExists, "already exists"},
		{ErrInvalidInput, "invalid input"},
		{ErrPermission, "permission denied"},
		{ErrTimeout, "operation timed out"},
		{ErrCancelled, "operation cancelled"},
		{ErrConflict, "conflict"},
		{ErrNotInitialized, "not initialized"},
		{ErrBoundaryViolation, "boundary violation"},
		{ErrGitFailed, "git operation failed"},
		{ErrCommandFailed, "command failed"},
	}
	for _, tt := range sentinels {
		t.Run(tt.text, func(t *testing.T) {
			if tt.err.Error() != tt.text {
				t.Errorf("sentinel error text = %q, want %q", tt.err.Error(), tt.text)
			}
		})
	}
}

func TestBoundaryError(t *testing.T) {
	inner := errors.New("symlink escape")

	t.Run("Error format", func(t *testing.T) {
		e := NewBoundary("validate", "/tmp/evil", "/home/user/campaign", inner)
		got := e.Error()
		if !strings.Contains(got, "boundary violation") {
			t.Errorf("Error() missing 'boundary violation': %q", got)
		}
		if !strings.Contains(got, "/tmp/evil") {
			t.Errorf("Error() missing path: %q", got)
		}
		if !strings.Contains(got, "/home/user/campaign") {
			t.Errorf("Error() missing root: %q", got)
		}
		if !strings.Contains(got, "validate") {
			t.Errorf("Error() missing op: %q", got)
		}
	})

	t.Run("Unwrap returns inner error", func(t *testing.T) {
		e := NewBoundary("remove", "/bad", "/root", inner)
		if !errors.Is(e, inner) {
			t.Error("errors.Is(e, inner) = false, want true")
		}
	})

	t.Run("errors.Is matches ErrBoundaryViolation sentinel", func(t *testing.T) {
		e := NewBoundary("write", "/bad", "/root", nil)
		if !Is(e, ErrBoundaryViolation) {
			t.Error("errors.Is(e, ErrBoundaryViolation) = false, want true")
		}
	})

	t.Run("errors.As extracts BoundaryError", func(t *testing.T) {
		e := NewBoundary("read", "/outside", "/campaign", nil)
		wrapped := fmt.Errorf("operation failed: %w", e)
		var be *BoundaryError
		if !As(wrapped, &be) {
			t.Fatal("errors.As failed to extract *BoundaryError")
		}
		if be.Path != "/outside" {
			t.Errorf("be.Path = %q, want %q", be.Path, "/outside")
		}
		if be.Op != "read" {
			t.Errorf("be.Op = %q, want %q", be.Op, "read")
		}
	})

	t.Run("nil Err does not panic", func(t *testing.T) {
		e := NewBoundary("test", "/p", "/r", nil)
		_ = e.Error()
		if e.Unwrap() != nil {
			t.Error("Unwrap() of nil Err should return nil")
		}
	})
}

func TestGitError(t *testing.T) {
	inner := errors.New("exit status 128")

	t.Run("Error with all fields", func(t *testing.T) {
		e := NewGit("commit", "/repo", "lock", "index.lock exists", inner)
		got := e.Error()
		if !strings.Contains(got, "git commit failed") {
			t.Errorf("missing op: %q", got)
		}
		if !strings.Contains(got, "(lock)") {
			t.Errorf("missing errType: %q", got)
		}
		if !strings.Contains(got, "/repo") {
			t.Errorf("missing path: %q", got)
		}
		if !strings.Contains(got, "index.lock exists") {
			t.Errorf("missing detail: %q", got)
		}
	})

	t.Run("Error with unknown type omits parenthetical", func(t *testing.T) {
		e := NewGit("add", "", "unknown", "", nil)
		got := e.Error()
		if strings.Contains(got, "(unknown)") {
			t.Errorf("should not include '(unknown)': %q", got)
		}
	})

	t.Run("Unwrap returns inner error", func(t *testing.T) {
		e := NewGit("fetch", "", "network", "", inner)
		if !errors.Is(e, inner) {
			t.Error("errors.Is(e, inner) = false, want true")
		}
	})

	t.Run("errors.Is matches ErrGitFailed sentinel", func(t *testing.T) {
		e := NewGit("push", "", "permission", "", nil)
		if !Is(e, ErrGitFailed) {
			t.Error("errors.Is(e, ErrGitFailed) = false, want true")
		}
	})

	t.Run("errors.As extracts GitError", func(t *testing.T) {
		e := NewGit("checkout", "/path", "not_repo", "not a git repo", nil)
		wrapped := fmt.Errorf("wrap: %w", e)
		var ge *GitError
		if !As(wrapped, &ge) {
			t.Fatal("errors.As failed")
		}
		if ge.Op != "checkout" {
			t.Errorf("ge.Op = %q, want %q", ge.Op, "checkout")
		}
		if ge.ErrType != "not_repo" {
			t.Errorf("ge.ErrType = %q, want %q", ge.ErrType, "not_repo")
		}
	})
}

func TestCommandError(t *testing.T) {
	inner := errors.New("exec: no such file")

	t.Run("ExitCode failure Error format", func(t *testing.T) {
		e := NewCommand("just build", 1, "build failed\n", nil)
		got := e.Error()
		if !strings.Contains(got, "just build") {
			t.Errorf("missing command: %q", got)
		}
		if !strings.Contains(got, "code 1") {
			t.Errorf("missing exit code: %q", got)
		}
		if !strings.Contains(got, "build failed") {
			t.Errorf("missing stderr: %q", got)
		}
	})

	t.Run("Launch failure Error format", func(t *testing.T) {
		e := NewCommand("unknown-binary", 0, "", inner)
		got := e.Error()
		if !strings.Contains(got, "unknown-binary") {
			t.Errorf("missing command: %q", got)
		}
		if !strings.Contains(got, "exec: no such file") {
			t.Errorf("missing inner error: %q", got)
		}
	})

	t.Run("Unwrap returns inner error", func(t *testing.T) {
		e := NewCommand("ls", 0, "", inner)
		if !errors.Is(e, inner) {
			t.Error("errors.Is(e, inner) = false, want true")
		}
	})

	t.Run("errors.Is matches ErrCommandFailed", func(t *testing.T) {
		e := NewCommand("make", 2, "", nil)
		if !Is(e, ErrCommandFailed) {
			t.Error("errors.Is(e, ErrCommandFailed) = false, want true")
		}
	})

	t.Run("errors.As extracts CommandError", func(t *testing.T) {
		e := NewCommand("go test", 1, "FAIL", nil)
		wrapped := fmt.Errorf("test run: %w", e)
		var ce *CommandError
		if !As(wrapped, &ce) {
			t.Fatal("errors.As failed to extract *CommandError")
		}
		if ce.ExitCode != 1 {
			t.Errorf("ce.ExitCode = %d, want 1", ce.ExitCode)
		}
		if ce.Stderr != "FAIL" {
			t.Errorf("ce.Stderr = %q, want %q", ce.Stderr, "FAIL")
		}
	})

	t.Run("Zero exit code no error", func(t *testing.T) {
		e := NewCommand("nothing", 0, "", nil)
		got := e.Error()
		if !strings.Contains(got, "nothing") {
			t.Errorf("fallback message missing command: %q", got)
		}
	})
}
