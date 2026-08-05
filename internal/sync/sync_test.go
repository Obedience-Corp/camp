package sync

import (
	"testing"
)

func TestValidateSubmoduleStatusOutputPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
		wantOp  string
	}{
		{
			name:   "clean",
			output: " abc1234 projects/foo (heads/main)",
		},
		{
			name:    "not initialized",
			output:  "-abc1234 projects/foo",
			wantErr: true,
			wantOp:  "validate",
		},
		{
			name:    "drift",
			output:  "+abc1234 projects/foo (heads/main)",
			wantErr: true,
			wantOp:  "validate-drift",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubmoduleStatusOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatal("validateSubmoduleStatusOutput() error = nil, want error")
				}
				syncErr, ok := err.(*SyncError)
				if !ok {
					t.Fatalf("validateSubmoduleStatusOutput() error type = %T, want *SyncError", err)
				}
				if syncErr.Op != tt.wantOp {
					t.Errorf("SyncError.Op = %q, want %q", syncErr.Op, tt.wantOp)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSubmoduleStatusOutput() error = %v, want nil", err)
			}
		})
	}
}

func TestCollectWarnings(t *testing.T) {
	syncer := NewSyncer("/tmp/test", WithForce(true))

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
		DetachedHEADs: []DetachedHEADStatus{
			{Path: "projects/detached", LocalCommits: 3, HasLocalWork: true},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 2 {
		t.Fatalf("collectWarnings() = %d warnings, want 2", len(warnings))
	}

	// Check for uncommitted changes warning
	hasUncommitted := false
	hasDetached := false
	for _, w := range warnings {
		if contains(w, "uncommitted") && contains(w, "projects/dirty") {
			hasUncommitted = true
		}
		if contains(w, "detached HEAD") && contains(w, "projects/detached") {
			hasDetached = true
		}
	}

	if !hasUncommitted {
		t.Error("expected uncommitted changes warning")
	}
	if !hasDetached {
		t.Error("expected detached HEAD warning")
	}
}

func TestCollectWarnings_SafeMode(t *testing.T) {
	// In safe mode (no force), uncommitted changes should NOT become warnings
	// because they cause the sync to abort
	syncer := NewSyncer("/tmp/test") // No force

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 0 {
		t.Errorf("collectWarnings() in safe mode = %d warnings, want 0", len(warnings))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestPullArtifacts_ConfigLoadExitContract pins the exit-code contract for an
// unreadable artifacts config: --artifacts-only fails the run (the artifacts
// were the whole ask), a default --from sync degrades to a warning and keeps
// exit 0 (the accelerated path must never be worse than a plain sync).
