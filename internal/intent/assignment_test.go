package intent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestIntentService_Claim(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    ClaimOptions
		wantErr bool
		checks  func(t *testing.T, claimed *Intent)
	}{
		{
			name:    "missing agent is rejected",
			opts:    ClaimOptions{},
			wantErr: true,
		},
		{
			name:    "blank agent is rejected",
			opts:    ClaimOptions{Agent: "   "},
			wantErr: true,
		},
		{
			name: "stamps assigned_to and assigned_at",
			opts: ClaimOptions{Agent: "claude-code-session-1"},
			checks: func(t *testing.T, claimed *Intent) {
				if claimed.AssignedTo != "claude-code-session-1" {
					t.Errorf("AssignedTo = %q, want %q", claimed.AssignedTo, "claude-code-session-1")
				}
				if claimed.AssignedAt.IsZero() {
					t.Error("AssignedAt should be stamped, got zero value")
				}
				if claimed.UpdatedAt.IsZero() {
					t.Error("UpdatedAt should be stamped on claim, got zero value")
				}
			},
		},
		{
			name: "merges refs into work_ref",
			opts: ClaimOptions{Agent: "claude-code-session-1", Refs: []string{"https://github.com/Obedience-Corp/camp/pull/123", ""}},
			checks: func(t *testing.T, claimed *Intent) {
				if len(claimed.WorkRef) != 1 || claimed.WorkRef[0] != "https://github.com/Obedience-Corp/camp/pull/123" {
					t.Errorf("WorkRef = %#v, want single PR URL (blank ref dropped)", claimed.WorkRef)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
			created, err := svc.CreateDirect(ctx, CreateOptions{
				Title:     "Claimable intent",
				Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("CreateDirect() error = %v", err)
			}

			claimed, err := svc.Claim(ctx, created.ID, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Claim() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.checks != nil {
				tt.checks(t, claimed)
			}

			// Round-trip: reload from disk to confirm the assignment persisted.
			reloaded, err := svc.Get(ctx, created.ID)
			if err != nil {
				t.Fatalf("Get() after claim error = %v", err)
			}
			if reloaded.AssignedTo != claimed.AssignedTo {
				t.Errorf("reloaded AssignedTo = %q, want %q", reloaded.AssignedTo, claimed.AssignedTo)
			}
		})
	}
}

func TestIntentService_Claim_ReclaimMergesRefsWithoutDroppingExisting(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	created, err := svc.CreateDirect(ctx, CreateOptions{Title: "Reclaim me", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	firstClaim, err := svc.Claim(ctx, created.ID, ClaimOptions{Agent: "session-1", Refs: []string{"branch/feature-x"}})
	if err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	firstAssignedAt := firstClaim.AssignedAt

	time.Sleep(time.Millisecond)

	secondClaim, err := svc.Claim(ctx, created.ID, ClaimOptions{
		Agent: "session-1",
		Refs:  []string{"https://github.com/Obedience-Corp/camp/pull/456"},
	})
	if err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}

	if len(secondClaim.WorkRef) != 2 {
		t.Fatalf("WorkRef = %#v, want both the branch and the PR URL retained", secondClaim.WorkRef)
	}
	if !secondClaim.AssignedAt.After(firstAssignedAt) {
		t.Error("re-claiming should re-stamp assigned_at to reflect the fresh claim")
	}
}

func TestIntentService_Claim_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	if _, err := svc.Claim(ctx, "does-not-exist", ClaimOptions{Agent: "session-1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Claim() on missing intent error = %v, want ErrNotFound", err)
	}
}

func TestIntentService_Claim_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Claim(ctx, "any-id", ClaimOptions{Agent: "session-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Claim() with cancelled context error = %v, want context.Canceled", err)
	}
}

func TestIntentService_Release(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	created, err := svc.CreateDirect(ctx, CreateOptions{Title: "Release me", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	if _, err := svc.Claim(ctx, created.ID, ClaimOptions{
		Agent: "session-1",
		Refs:  []string{"https://github.com/Obedience-Corp/camp/pull/789"},
	}); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	released, err := svc.Release(ctx, created.ID)
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	if released.AssignedTo != "" {
		t.Errorf("AssignedTo = %q, want empty after release", released.AssignedTo)
	}
	if !released.AssignedAt.IsZero() {
		t.Errorf("AssignedAt = %v, want zero after release", released.AssignedAt)
	}
	if len(released.WorkRef) != 1 || released.WorkRef[0] != "https://github.com/Obedience-Corp/camp/pull/789" {
		t.Errorf("WorkRef = %#v, want the PR URL preserved so sync can still resolve it", released.WorkRef)
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after release error = %v", err)
	}
	if reloaded.AssignedTo != "" {
		t.Errorf("reloaded AssignedTo = %q, want empty", reloaded.AssignedTo)
	}
}

func TestIntentService_Release_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	if _, err := svc.Release(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Release() on missing intent error = %v, want ErrNotFound", err)
	}
}

func TestIntentService_Release_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Release(ctx, "any-id")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Release() with cancelled context error = %v, want context.Canceled", err)
	}
}

func TestIntentService_Release_AlreadyUnclaimedIsNoop(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	created, err := svc.CreateDirect(ctx, CreateOptions{Title: "Never claimed", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	released, err := svc.Release(ctx, created.ID)
	if err != nil {
		t.Fatalf("Release() on unclaimed intent error = %v", err)
	}
	if released.AssignedTo != "" {
		t.Errorf("AssignedTo = %q, want empty", released.AssignedTo)
	}
}

func TestMergeWorkRefs(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		add      []string
		want     []string
	}{
		{
			name: "nil in nil out",
			want: nil,
		},
		{
			name:     "drops blanks and duplicates while preserving order",
			existing: []string{"branch/a", "  ", "https://github.com/o/r/pull/1"},
			add:      []string{"https://github.com/o/r/pull/1", "branch/b"},
			want:     []string{"branch/a", "https://github.com/o/r/pull/1", "branch/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeWorkRefs(tt.existing, tt.add)
			if len(got) != len(tt.want) {
				t.Fatalf("mergeWorkRefs() = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("mergeWorkRefs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
