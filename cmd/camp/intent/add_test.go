package intent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
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
