package promote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestPromoteToReady(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	intentsDir := filepath.Join(campaignRoot, "workflow", "intents")
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:  "Prepare onboarding flow",
		Type:   intent.TypeFeature,
		Author: "test",
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	got, err := Promote(ctx, svc, created, Options{
		CampaignRoot: campaignRoot,
		Target:       TargetReady,
	})
	if err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if got.NewStatus != intent.StatusReady {
		t.Fatalf("NewStatus = %q, want %q", got.NewStatus, intent.StatusReady)
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Status != intent.StatusReady {
		t.Fatalf("Status = %q, want %q", reloaded.Status, intent.StatusReady)
	}
	if reloaded.PromotedTo != "" {
		t.Fatalf("PromotedTo = %q, want empty", reloaded.PromotedTo)
	}
}

func TestPromoteToDesign(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	intentsDir := filepath.Join(campaignRoot, "workflow", "intents")
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:  "Design API request signing flow",
		Type:   intent.TypeResearch,
		Author: "test",
		Body:   "We need a clear signing strategy with replay protection.",
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}
	ready, err := svc.Move(ctx, created.ID, intent.StatusReady)
	if err != nil {
		t.Fatalf("Move() to ready error = %v", err)
	}

	got, err := Promote(ctx, svc, ready, Options{
		CampaignRoot: campaignRoot,
		Target:       TargetDesign,
	})
	if err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if got.NewStatus != intent.StatusActive {
		t.Fatalf("NewStatus = %q, want %q", got.NewStatus, intent.StatusActive)
	}
	if !got.DesignCreated {
		t.Fatalf("DesignCreated = false, want true")
	}
	if got.DesignDir == "" {
		t.Fatalf("DesignDir = empty, want non-empty")
	}

	readmePath := filepath.Join(campaignRoot, got.DesignDir, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("design README missing at %s: %v", readmePath, err)
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Status != intent.StatusActive {
		t.Fatalf("Status = %q, want %q", reloaded.Status, intent.StatusActive)
	}
	if reloaded.PromotedTo != got.DesignDir {
		t.Fatalf("PromotedTo = %q, want %q", reloaded.PromotedTo, got.DesignDir)
	}
}

func TestPromoteToDesignRequiresReadyWithoutForce(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	intentsDir := filepath.Join(campaignRoot, "workflow", "intents")
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:  "Designing metrics schema",
		Type:   intent.TypeResearch,
		Author: "test",
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	_, err = Promote(ctx, svc, created, Options{
		CampaignRoot: campaignRoot,
		Target:       TargetDesign,
		Force:        false,
	})
	if err == nil {
		t.Fatal("Promote() expected error when promoting non-ready intent to design")
	}
}

func TestValidTargetsForStatus(t *testing.T) {
	tests := []struct {
		status intent.Status
		want   []Target
	}{
		{status: intent.StatusInbox, want: []Target{TargetReady}},
		{status: intent.StatusReady, want: []Target{TargetFestival, TargetDesign}},
		{status: intent.StatusActive, want: nil},
		{status: intent.StatusDone, want: nil},
	}

	for _, tt := range tests {
		got := ValidTargetsForStatus(tt.status)
		if len(got) != len(tt.want) {
			t.Fatalf("ValidTargetsForStatus(%q) len = %d, want %d", tt.status, len(got), len(tt.want))
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("ValidTargetsForStatus(%q)[%d] = %q, want %q", tt.status, i, got[i], tt.want[i])
			}
		}
	}
}

