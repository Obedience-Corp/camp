package project

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func testProjectAddRegistry(t *testing.T) *config.Registry {
	t.Helper()

	reg := config.NewRegistry()
	if err := reg.Register("current-1111", "current", "/campaigns/current", config.CampaignTypeProduct); err != nil {
		t.Fatalf("Register(current) error = %v", err)
	}
	if err := reg.Register("alpha-2222", "alpha", "/campaigns/alpha", config.CampaignTypeProduct); err != nil {
		t.Fatalf("Register(alpha) error = %v", err)
	}
	if err := reg.Register("beta-3333", "beta", "/campaigns/beta", config.CampaignTypeResearch); err != nil {
		t.Fatalf("Register(beta) error = %v", err)
	}

	return reg
}

func testProjectAddLoadCampaign(path string) (*config.CampaignConfig, error) {
	switch path {
	case "/campaigns/current":
		return &config.CampaignConfig{ID: "current-1111", Name: "current", Type: config.CampaignTypeProduct}, nil
	case "/campaigns/alpha":
		return &config.CampaignConfig{ID: "alpha-2222", Name: "alpha", Type: config.CampaignTypeProduct}, nil
	case "/campaigns/beta":
		return &config.CampaignConfig{ID: "beta-3333", Name: "beta", Type: config.CampaignTypeResearch}, nil
	default:
		return &config.CampaignConfig{ID: "unknown", Name: path, Type: config.CampaignTypeProduct}, nil
	}
}

func TestProjectAddCampaignResolver_CurrentCampaignFromCwd(t *testing.T) {
	wantCfg := &config.CampaignConfig{ID: "current-1111", Name: "current"}
	reg := testProjectAddRegistry(t)

	r := projectAddCampaignResolver{
		loadCurrent: func(context.Context) (*config.CampaignConfig, string, error) {
			return wantCfg, "/campaigns/current", nil
		},
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
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

func TestProjectAddCampaignResolver_CurrentCampaignAllowsUnregisteredRoot(t *testing.T) {
	r := projectAddCampaignResolver{
		loadCurrent: func(context.Context) (*config.CampaignConfig, string, error) {
			return &config.CampaignConfig{ID: "current-1111", Name: "current"}, "/campaigns/current", nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "", false)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if root != "/campaigns/current" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/current")
	}
	if cfg == nil || cfg.ID != "current-1111" {
		t.Fatalf("cfg = %#v, want current campaign config", cfg)
	}
}

func TestProjectAddCampaignResolver_NoCurrentCampaignUsesPicker(t *testing.T) {
	reg := testProjectAddRegistry(t)
	pickCalls := 0

	r := projectAddCampaignResolver{
		isInteractive: func() bool { return true },
		loadCurrent: func(context.Context) (*config.CampaignConfig, string, error) {
			return nil, "", errors.New("not inside a campaign")
		},
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
		loadCampaign: func(context.Context, string) (*config.CampaignConfig, error) {
			return testProjectAddLoadCampaign("/campaigns/alpha")
		},
		saveRegistry: func(context.Context, *config.Registry) error { return nil },
		pickCampaign: func(context.Context, *config.Registry) (config.RegisteredCampaign, error) {
			pickCalls++
			c, _ := reg.GetByName("alpha")
			return c, nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "", false)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if pickCalls != 1 {
		t.Fatalf("pickCampaign calls = %d, want 1", pickCalls)
	}
	if root != "/campaigns/alpha" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/alpha")
	}
	if cfg.Name != "alpha" {
		t.Fatalf("cfg.Name = %q, want %q", cfg.Name, "alpha")
	}
}

func TestProjectAddCampaignResolver_NoCurrentCampaignNonInteractiveFails(t *testing.T) {
	reg := testProjectAddRegistry(t)

	r := projectAddCampaignResolver{
		isInteractive: func() bool { return false },
		loadCurrent: func(context.Context) (*config.CampaignConfig, string, error) {
			return nil, "", errors.New("not inside a campaign")
		},
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
	}

	_, _, err := r.resolve(context.Background(), "", false)
	if err == nil {
		t.Fatal("resolve() expected error outside a campaign in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "campaign name required in non-interactive mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectAddCampaignResolver_ExplicitCampaignLookup(t *testing.T) {
	reg := testProjectAddRegistry(t)
	var matched bytes.Buffer
	saveCalls := 0

	r := projectAddCampaignResolver{
		stderr:        &matched,
		isInteractive: func() bool { return true },
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
		loadCampaign: func(context.Context, string) (*config.CampaignConfig, error) {
			return testProjectAddLoadCampaign("/campaigns/beta")
		},
		saveRegistry: func(context.Context, *config.Registry) error {
			saveCalls++
			return nil
		},
	}

	cfg, root, err := r.resolve(context.Background(), "beta", true)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if root != "/campaigns/beta" {
		t.Fatalf("root = %q, want %q", root, "/campaigns/beta")
	}
	if cfg.Name != "beta" {
		t.Fatalf("cfg.Name = %q, want %q", cfg.Name, "beta")
	}
	if saveCalls != 1 {
		t.Fatalf("saveRegistry calls = %d, want 1", saveCalls)
	}
	if matched.Len() != 0 {
		t.Fatalf("unexpected fuzzy match output: %q", matched.String())
	}
}

func TestProjectAddCampaignResolver_BareCampaignUsesPicker(t *testing.T) {
	reg := testProjectAddRegistry(t)
	pickCalls := 0

	r := projectAddCampaignResolver{
		isInteractive: func() bool { return true },
		loadRegistry: func(context.Context) (*config.Registry, error) {
			return reg, nil
		},
		loadCampaign: func(context.Context, string) (*config.CampaignConfig, error) {
			return testProjectAddLoadCampaign("/campaigns/alpha")
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
	if cfg.Name != "alpha" {
		t.Fatalf("cfg.Name = %q, want %q", cfg.Name, "alpha")
	}
}

func TestNormalizeProjectAddCampaignArgs_ExplicitCampaignAndRemoteSource(t *testing.T) {
	targetCampaign, args := normalizeProjectAddCampaignArgs(
		[]string{"beta", "git@github.com:org/repo.git"},
		noOptProjectAddCampaign,
		"",
		"",
	)

	if targetCampaign != "beta" {
		t.Fatalf("targetCampaign = %q, want %q", targetCampaign, "beta")
	}
	if len(args) != 1 || args[0] != "git@github.com:org/repo.git" {
		t.Fatalf("args = %#v, want only the remote source", args)
	}
}

func TestNormalizeProjectAddCampaignArgs_BareCampaignKeepsSourceLikeArg(t *testing.T) {
	targetCampaign, args := normalizeProjectAddCampaignArgs(
		[]string{"git@github.com:org/repo.git"},
		noOptProjectAddCampaign,
		"",
		"",
	)

	if targetCampaign != "" {
		t.Fatalf("targetCampaign = %q, want empty picker target", targetCampaign)
	}
	if len(args) != 1 || args[0] != "git@github.com:org/repo.git" {
		t.Fatalf("args = %#v, want original source argument", args)
	}
}
