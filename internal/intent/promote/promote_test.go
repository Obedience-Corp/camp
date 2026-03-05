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

func TestCreateDesignDoc_UsesUniqueDirectoryPerIntentID(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	first := &intent.Intent{
		ID:      "design-api-request-signing-flow-20260304-120000",
		Title:   "Design API request signing flow",
		Content: "First design content.",
	}
	second := &intent.Intent{
		ID:      "design-api-request-signing-flow-20260304-120001",
		Title:   "Design API request signing flow",
		Content: "Second design content.",
	}

	firstDir, createdFirst, err := createDesignDoc(ctx, campaignRoot, first)
	if err != nil {
		t.Fatalf("createDesignDoc(first) error = %v", err)
	}
	if !createdFirst {
		t.Fatalf("createDesignDoc(first) should create a new README")
	}
	secondDir, createdSecond, err := createDesignDoc(ctx, campaignRoot, second)
	if err != nil {
		t.Fatalf("createDesignDoc(second) error = %v", err)
	}
	if !createdSecond {
		t.Fatalf("createDesignDoc(second) should create a new README")
	}

	if firstDir == secondDir {
		t.Fatalf("design directories should differ, both were %q", firstDir)
	}

	wantFirst := filepath.Join("workflow", "design", "design-api-request-signing-flow-20260304-120000")
	wantSecond := filepath.Join("workflow", "design", "design-api-request-signing-flow-20260304-120001")
	if firstDir != wantFirst {
		t.Fatalf("firstDir = %q, want %q", firstDir, wantFirst)
	}
	if secondDir != wantSecond {
		t.Fatalf("secondDir = %q, want %q", secondDir, wantSecond)
	}
}

func TestCreateDesignDoc_DoesNotOverwriteExistingReadme(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	i := &intent.Intent{
		ID:      "design-api-request-signing-flow-20260304-120000",
		Title:   "Design API request signing flow",
		Content: "Original design content.",
	}

	designDir, created, err := createDesignDoc(ctx, campaignRoot, i)
	if err != nil {
		t.Fatalf("createDesignDoc() error = %v", err)
	}
	if !created {
		t.Fatalf("createDesignDoc() should create README on first call")
	}

	readmePath := filepath.Join(campaignRoot, designDir, "README.md")
	customContent := []byte("# Existing Design\n\nKeep this content.\n")
	if err := os.WriteFile(readmePath, customContent, 0644); err != nil {
		t.Fatalf("WriteFile(custom README) error = %v", err)
	}

	i.Content = "Changed content that should not overwrite."
	designDir2, created2, err := createDesignDoc(ctx, campaignRoot, i)
	if err != nil {
		t.Fatalf("createDesignDoc(second call) error = %v", err)
	}
	if designDir2 != designDir {
		t.Fatalf("designDir2 = %q, want %q", designDir2, designDir)
	}
	if created2 {
		t.Fatalf("createDesignDoc(second call) should not rewrite existing README")
	}

	gotContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("ReadFile(README) error = %v", err)
	}
	if string(gotContent) != string(customContent) {
		t.Fatalf("README was overwritten:\n got: %q\nwant: %q", string(gotContent), string(customContent))
	}
}

func TestPromoteToDesign_IsTransactionalOnDesignDocFailure(t *testing.T) {
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

	workflowDir := filepath.Join(campaignRoot, "workflow")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("MkdirAll(workflow) error = %v", err)
	}
	// Block design directory creation by creating a file where the directory should be.
	if err := os.WriteFile(filepath.Join(workflowDir, "design"), []byte("blocked"), 0644); err != nil {
		t.Fatalf("WriteFile(workflow/design blocker) error = %v", err)
	}

	_, err = Promote(ctx, svc, ready, Options{
		CampaignRoot: campaignRoot,
		Target:       TargetDesign,
	})
	if err == nil {
		t.Fatal("Promote() expected error when design directory creation fails")
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

func TestIntentIDTimestampSuffix(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "valid generated id",
			id:   "design-api-request-signing-flow-20260304-120000",
			want: "20260304-120000",
		},
		{
			name: "timestamp only id",
			id:   "20260304-120000",
			want: "20260304-120000",
		},
		{
			name: "invalid suffix",
			id:   "design-api-request-signing-flow-20260304-12ab00",
			want: "",
		},
		{
			name: "too short",
			id:   "bad-id",
			want: "",
		},
	}

	for _, tt := range tests {
		got := intentIDTimestampSuffix(tt.id)
		if got != tt.want {
			t.Fatalf("%s: intentIDTimestampSuffix(%q) = %q, want %q", tt.name, tt.id, got, tt.want)
		}
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
