package main

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestRegisterCampaignWithConfirmedConflictsReplacesApprovedEntries(t *testing.T) {
	reg := config.NewRegistry()
	mustRegister(t, reg, "name-conflict-id", "target", "/old-target", config.CampaignTypeProduct)
	mustRegister(t, reg, "path-conflict-id", "other", "/target", config.CampaignTypeResearch)

	err := registerCampaignWithConfirmedConflicts(
		reg,
		"target-id",
		"target",
		"/target",
		config.CampaignTypeTools,
		registerConflictConfirmations{
			nameConflictID: "name-conflict-id",
			pathConflictID: "path-conflict-id",
		},
	)
	if err != nil {
		t.Fatalf("registerCampaignWithConfirmedConflicts() error = %v", err)
	}

	if _, ok := reg.GetByID("name-conflict-id"); ok {
		t.Fatal("name conflict was not removed")
	}
	if _, ok := reg.GetByID("path-conflict-id"); ok {
		t.Fatal("path conflict was not removed")
	}
	got, ok := reg.GetByID("target-id")
	if !ok {
		t.Fatal("target campaign was not registered")
	}
	if got.Name != "target" || got.Path != "/target" || got.Type != config.CampaignTypeTools {
		t.Fatalf("registered campaign = %+v, want target at /target with tools type", got)
	}
}

func TestRegisterCampaignWithConfirmedConflictsRejectsChangedConflict(t *testing.T) {
	reg := config.NewRegistry()
	mustRegister(t, reg, "new-name-conflict-id", "target", "/new-target", config.CampaignTypeProduct)

	err := registerCampaignWithConfirmedConflicts(
		reg,
		"target-id",
		"target",
		"/target",
		config.CampaignTypeTools,
		registerConflictConfirmations{nameConflictID: "old-name-conflict-id"},
	)
	if err == nil {
		t.Fatal("registerCampaignWithConfirmedConflicts() error = nil, want changed conflict error")
	}
	if !strings.Contains(err.Error(), "registration changed") {
		t.Fatalf("error = %q, want changed registration message", err)
	}
	if _, ok := reg.GetByID("new-name-conflict-id"); !ok {
		t.Fatal("changed name conflict should not be removed")
	}
	if _, ok := reg.GetByID("target-id"); ok {
		t.Fatal("target campaign should not be registered after changed conflict")
	}
}

func mustRegister(t *testing.T, reg *config.Registry, id, name, path string, ctype config.CampaignType) {
	t.Helper()
	if err := reg.Register(id, name, path, ctype); err != nil {
		t.Fatalf("Register(%q, %q, %q) error = %v", id, name, path, err)
	}
}
