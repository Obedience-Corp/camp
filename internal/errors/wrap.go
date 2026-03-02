package errors

import (
	"errors"
	"fmt"
)

// Wrap adds context to an error using fmt.Errorf with %w.
// Returns nil if err is nil.
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf adds formatted context to an error.
// Returns nil if err is nil.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WrapJoin creates an error that wraps both sentinel and cause in the error chain.
// This preserves errors.Is matching for both errors, unlike Wrap which only chains one.
// Use when an operation has both a categorical sentinel (e.g., ErrSubmoduleInit) and
// an underlying cause (e.g., exec error).
// Returns nil if both sentinel and cause are nil.
func WrapJoin(sentinel, cause error, msg string) error {
	if sentinel == nil && cause == nil {
		return nil
	}
	if sentinel == nil {
		return Wrap(cause, msg)
	}
	if cause == nil {
		return Wrap(sentinel, msg)
	}
	if msg == "" {
		return fmt.Errorf("%w: %w", sentinel, cause)
	}
	return fmt.Errorf("%s: %w: %w", msg, sentinel, cause)
}

// WrapJoinf is like WrapJoin but accepts a format string for the message.
func WrapJoinf(sentinel, cause error, format string, args ...any) error {
	return WrapJoin(sentinel, cause, fmt.Sprintf(format, args...))
}

// Is delegates to errors.Is for convenience.
func Is(err, target error) bool { return errors.Is(err, target) }

// As delegates to errors.As for convenience.
func As(err error, target any) bool { return errors.As(err, target) }

// Unwrap delegates to errors.Unwrap for convenience.
func Unwrap(err error) error { return errors.Unwrap(err) }

// New delegates to errors.New for convenience.
func New(text string) error { return errors.New(text) }

// Join delegates to errors.Join for convenience.
func Join(errs ...error) error { return errors.Join(errs...) }
