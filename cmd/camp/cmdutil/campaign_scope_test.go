package cmdutil

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func testRegistry(campaigns ...config.RegisteredCampaign) *config.Registry {
	reg := config.NewRegistry()
	for _, c := range campaigns {
		if c.Org == "" {
			c.Org = config.DefaultOrg
		}
		if c.Status == "" {
			c.Status = config.StatusActive
		}
		reg.Campaigns[c.ID] = c
	}
	return reg
}

func TestResolveCampaignSelection_AmbiguousIDPrefixErrors(t *testing.T) {
	reg := testRegistry(
		config.RegisteredCampaign{ID: "same-alpha", Name: "alpha", Path: "/tmp/alpha"},
		config.RegisteredCampaign{ID: "same-beta", Name: "beta", Path: "/tmp/beta"},
	)

	_, err := ResolveCampaignSelection("same", reg, nil)
	if err == nil {
		t.Fatal("expected ambiguous ID prefix error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %q, want ambiguous", err.Error())
	}
}

func TestResolveCampaignSelection_DuplicateExactNamesError(t *testing.T) {
	reg := testRegistry(
		config.RegisteredCampaign{ID: "id-alpha", Name: "shared", Path: "/tmp/alpha"},
		config.RegisteredCampaign{ID: "id-beta", Name: "shared", Path: "/tmp/beta"},
	)

	_, err := ResolveCampaignSelection("shared", reg, nil)
	if err == nil {
		t.Fatal("expected duplicate exact name error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %q, want ambiguous", err.Error())
	}
}

func TestResolveCampaignSelection_LegacyCallersSeeAllStatuses(t *testing.T) {
	reg := testRegistry(
		config.RegisteredCampaign{
			ID:     "inactive-campaign",
			Name:   "archive",
			Path:   "/tmp/archive",
			Status: config.StatusInactive,
		},
	)

	got, err := ResolveCampaignSelection("archive", reg, nil)
	if err != nil {
		t.Fatalf("ResolveCampaignSelection inactive: %v", err)
	}
	if got.ID != "inactive-campaign" {
		t.Fatalf("resolved ID = %q, want inactive-campaign", got.ID)
	}
}
