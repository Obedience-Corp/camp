package intent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func testRegistry(t *testing.T) *config.Registry {
	t.Helper()

	reg := config.NewRegistry()
	if err := reg.Register("aaaa-1111", "alpha", "/campaigns/alpha", config.CampaignTypeProduct); err != nil {
		t.Fatalf("Register(alpha) error = %v", err)
	}
	if err := reg.Register("bbbb-2222", "beta", "/campaigns/beta", config.CampaignTypeResearch); err != nil {
		t.Fatalf("Register(beta) error = %v", err)
	}

	return reg
}

func testLoadCampaign(path string) (*config.CampaignConfig, error) {
	return &config.CampaignConfig{
		ID:   "cfg-" + path,
		Name: path,
		Type: config.CampaignTypeProduct,
	}, nil
}

func TestIntentAddCampaignResolver_CurrentCampaignFromCwd(t *testing.T) {
	wantCfg := &config.CampaignConfig{ID: "current", Name: "current"}
	r := intentAddCampaignResolver{
		loadCurrent: func(context.Context) (*config.CampaignConfig, string, error) {
			return wantCfg, "/campaigns/current", nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "", false)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if cfg != wantCfg {
		t.Fatalf("cfg mismatch: got %#v want %#v", cfg, wantCfg)
	}
	if root != "/campaigns/current" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/current")
	}
}

func TestIntentAddCampaignResolver_ExplicitCampaignLookup(t *testing.T) {
	reg := testRegistry(t)
	var matched bytes.Buffer
	saveCalls := 0

	r := intentAddCampaignResolver{
		stderr:        &matched,
		isInteractive: func() bool { return true },
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
		loadCampaign: func(ctx context.Context, path string) (*config.CampaignConfig, error) {
			return testLoadCampaign(path)
		},
		saveRegistry: func(context.Context, *config.Registry) error {
			saveCalls++
			return nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "bbbb", true)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if root != "/campaigns/beta" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/beta")
	}
	if cfg.Name != "/campaigns/beta" {
		t.Fatalf("cfg.Name = %q, want %q", cfg.Name, "/campaigns/beta")
	}
	if saveCalls != 1 {
		t.Fatalf("saveRegistry calls = %d, want 1", saveCalls)
	}
	if matched.Len() != 0 {
		t.Fatalf("unexpected fuzzy match output: %q", matched.String())
	}
}

func TestIntentAddCampaignResolver_BareCampaignUsesPicker(t *testing.T) {
	reg := testRegistry(t)
	pickCalls := 0

	r := intentAddCampaignResolver{
		isInteractive: func() bool { return true },
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
		loadCampaign: func(ctx context.Context, path string) (*config.CampaignConfig, error) {
			return testLoadCampaign(path)
		},
		saveRegistry: func(context.Context, *config.Registry) error { return nil },
		pickCampaign: func(context.Context, *config.Registry) (config.RegisteredCampaign, error) {
			pickCalls++
			c, _ := reg.GetByName("alpha")
			return c, nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "", true)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if pickCalls != 1 {
		t.Fatalf("pickCampaign calls = %d, want 1", pickCalls)
	}
	if root != "/campaigns/alpha" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/alpha")
	}
	if cfg.Name != "/campaigns/alpha" {
		t.Fatalf("cfg.Name = %q, want %q", cfg.Name, "/campaigns/alpha")
	}
}

func TestIntentAddCampaignResolver_BareCampaignNonInteractiveFails(t *testing.T) {
	reg := testRegistry(t)

	r := intentAddCampaignResolver{
		isInteractive: func() bool { return false },
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
	}

	_, _, err := r.resolve(context.Background(), "", true)
	if err == nil {
		t.Fatal("resolve() expected error for bare --campaign in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "campaign name required in non-interactive mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIntentAdd_TargetCampaignSmoke(t *testing.T) {
	ctx := context.Background()
	currentRoot := t.TempDir()
	targetRoot := t.TempDir()

	writeCampaign := func(root, id, name string) {
		cfg := &config.CampaignConfig{
			ID:        id,
			Name:      name,
			Type:      config.CampaignTypeProduct,
			CreatedAt: time.Now(),
		}
		if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
			t.Fatalf("SaveCampaignConfig(%s) error = %v", root, err)
		}
		jumps := config.DefaultJumpsConfig()
		if err := config.SaveJumpsConfig(ctx, root, &jumps); err != nil {
			t.Fatalf("SaveJumpsConfig(%s) error = %v", root, err)
		}
	}

	writeCampaign(currentRoot, "current-id", "current-campaign")
	writeCampaign(targetRoot, "target-id", "target-campaign")

	reg := config.NewRegistry()
	if err := reg.Register("current-id", "current-campaign", currentRoot, config.CampaignTypeProduct); err != nil {
		t.Fatalf("Register(current) error = %v", err)
	}
	if err := reg.Register("target-id", "target-campaign", targetRoot, config.CampaignTypeProduct); err != nil {
		t.Fatalf("Register(target) error = %v", err)
	}

	registryPath := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	if err := config.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(currentRoot); err != nil {
		t.Fatalf("Chdir(%s) error = %v", currentRoot, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	resolver := newIntentAddCampaignResolver(&bytes.Buffer{})
	cfg, campaignRoot, err := resolver.resolve(ctx, "target-campaign", true)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if campaignRoot != targetRoot {
		t.Fatalf("campaignRoot = %q, want %q", campaignRoot, targetRoot)
	}

	pathResolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentSvc := intentcore.NewIntentService(campaignRoot, pathResolver.Intents())
	if err := intentSvc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	if err := runFastCapture(ctx, intentSvc, pathResolver.Intents(), cfg, campaignRoot, true, intentcore.CreateOptions{
		Title:  "Targeted intent",
		Type:   intentcore.TypeIdea,
		Author: "agent",
	}); err != nil {
		t.Fatalf("runFastCapture() error = %v", err)
	}

	targetInbox := filepath.Join(targetRoot, ".campaign", "intents", "inbox")
	entries, err := os.ReadDir(targetInbox)
	if err != nil {
		t.Fatalf("ReadDir(target inbox) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("target inbox entries = %d, want 1", len(entries))
	}

	if _, err := os.Stat(filepath.Join(targetRoot, ".campaign", "intents", ".intents.jsonl")); err != nil {
		t.Fatalf("expected audit log in target campaign: %v", err)
	}

	currentInbox := filepath.Join(currentRoot, ".campaign", "intents", "inbox")
	currentEntries, err := os.ReadDir(currentInbox)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir(current inbox) error = %v", err)
	}
	if len(currentEntries) != 0 {
		t.Fatalf("current inbox entries = %d, want 0", len(currentEntries))
	}
}
