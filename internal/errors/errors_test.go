package errors

import (
	"context"
	"fmt"
	"io"
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
	}
	for _, tt := range sentinels {
		t.Run(tt.text, func(t *testing.T) {
			if tt.err.Error() != tt.text {
				t.Errorf("sentinel error text = %q, want %q", tt.err.Error(), tt.text)
			}
		})
	}
}
