package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsSigpipeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "EPIPE error",
			err:  syscall.EPIPE,
			want: true,
		},
		{
			name: "wrapped EPIPE error",
			err:  fmt.Errorf("write failed: %w", syscall.EPIPE),
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("something failed"),
			want: false,
		},
		{
			name: "os.PathError not EPIPE",
			err:  &os.PathError{Op: "write", Path: "stdout", Err: errors.New("other")},
			want: false,
		},
		{
			name: "os.PathError with EPIPE",
			err:  &os.PathError{Op: "write", Path: "stdout", Err: syscall.EPIPE},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSigpipeError(tt.err)
			if got != tt.want {
				t.Errorf("isSigpipeError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
