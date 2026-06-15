package registry

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestSyncRegistryCampaignWithConfirmedConflictReplacesApprovedEntry(t *testing.T) {
	reg := config.NewRegistry()
	mustRegister(t, reg, "conflict-id", "old-campaign", "/campaign", config.CampaignTypeResearch)
	cfg := &config.CampaignConfig{
		ID:   "campaign-id",
		Name: "campaign",
		Type: config.CampaignTypeProduct,
	}

	err := syncRegistryCampaignWithConfirmedConflict(reg, cfg, "/campaign", "conflict-id")
	if err != nil {
		t.Fatalf("syncRegistryCampaignWithConfirmedConflict() error = %v", err)
	}
	if _, ok := reg.GetByID("conflict-id"); ok {
		t.Fatal("conflicting campaign was not removed")
	}
	got, ok := reg.GetByID("campaign-id")
	if !ok {
		t.Fatal("campaign was not registered")
	}
	if got.Name != "campaign" || got.Path != "/campaign" || got.Type != config.CampaignTypeProduct {
		t.Fatalf("registered campaign = %+v, want campaign at /campaign with product type", got)
	}
}

func TestSyncRegistryCampaignWithConfirmedConflictRejectsChangedEntry(t *testing.T) {
	reg := config.NewRegistry()
	mustRegister(t, reg, "new-conflict-id", "new-campaign", "/campaign", config.CampaignTypeResearch)
	cfg := &config.CampaignConfig{
		ID:   "campaign-id",
		Name: "campaign",
		Type: config.CampaignTypeProduct,
	}

	err := syncRegistryCampaignWithConfirmedConflict(reg, cfg, "/campaign", "old-conflict-id")
	if err == nil {
		t.Fatal("syncRegistryCampaignWithConfirmedConflict() error = nil, want changed conflict error")
	}
	if !strings.Contains(err.Error(), "registration changed") {
		t.Fatalf("error = %q, want changed registration message", err)
	}
	if _, ok := reg.GetByID("new-conflict-id"); !ok {
		t.Fatal("changed conflict should not be removed")
	}
	if _, ok := reg.GetByID("campaign-id"); ok {
		t.Fatal("campaign should not be registered after changed conflict")
	}
}

func mustRegister(t *testing.T, reg *config.Registry, id, name, path string, ctype config.CampaignType) {
	t.Helper()
	if err := reg.Register(id, name, path, ctype); err != nil {
		t.Fatalf("Register(%q, %q, %q) error = %v", id, name, path, err)
	}
}
