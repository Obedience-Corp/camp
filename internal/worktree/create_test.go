package worktree

import (
	"errors"
	"testing"
)

func TestNewBranchConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		localExists  bool
		remoteExists bool
		wantNil      bool
		sentinel     error
	}{
		{name: "neither", wantNil: true},
		{name: "local leftover", localExists: true, sentinel: ErrBranchExists},
		{name: "origin shadow", remoteExists: true, sentinel: ErrRemoteBranchExists},
		{name: "local wins over origin", localExists: true, remoteExists: true, sentinel: ErrBranchExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newBranchConflict("camp", "judge-command-tools", tt.localExists, tt.remoteExists)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("newBranchConflict() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("newBranchConflict() = nil, want error")
			}
			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tt.sentinel)
			}
			if errors.Is(err, ErrBranchExists) && errors.Is(err, ErrRemoteBranchExists) {
				t.Fatal("sentinels must not match each other")
			}
		})
	}
}
