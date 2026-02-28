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
