package cmdutil

import (
	"strings"
	"testing"
	"time"

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

func tiedRegistry(oldAccess, newAccess time.Time) *config.Registry {
	return testRegistry(
		config.RegisteredCampaign{ID: "id-a", Name: "obey-campaign", Path: "/tmp/a", LastAccess: oldAccess},
		config.RegisteredCampaign{ID: "id-b", Name: "Obey-Agent-Economy", Path: "/tmp/b", LastAccess: newAccess},
		config.RegisteredCampaign{ID: "id-c", Name: "unrelated", Path: "/tmp/c"},
	)
}

func TestResolveCampaignSelection_TieBreaksByRecencyDeterministically(t *testing.T) {
	now := time.Now()
	for i := 0; i < 200; i++ {
		reg := tiedRegistry(now.Add(-time.Hour), now)
		got, err := ResolveCampaignSelection("obey-", reg, nil)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if got.ID != "id-b" {
			t.Fatalf("iteration %d: got %s, want id-b (most recent)", i, got.ID)
		}
		reg = tiedRegistry(now, now.Add(-time.Hour))
		got, _ = ResolveCampaignSelection("obey-", reg, nil)
		if got.ID != "id-a" {
			t.Fatalf("iteration %d: got %s, want id-a (most recent)", i, got.ID)
		}
	}
}

func TestResolveCampaignSelection_EqualRecencyFallsBackToName(t *testing.T) {
	now := time.Now()
	for i := 0; i < 200; i++ {
		reg := tiedRegistry(now, now)
		got, err := ResolveCampaignSelection("obey-", reg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "id-b" {
			t.Fatalf("iteration %d: got %s, want Obey-Agent-Economy by name order", i, got.ID)
		}
	}
}

func TestResolveCampaignSelection_BetterMatchBeatsRecency(t *testing.T) {
	now := time.Now()
	reg := testRegistry(
		config.RegisteredCampaign{ID: "id-a", Name: "obey", Path: "/tmp/a", LastAccess: now.Add(-24 * time.Hour)},
		config.RegisteredCampaign{ID: "id-b", Name: "xobey", Path: "/tmp/b", LastAccess: now},
	)
	for i := 0; i < 50; i++ {
		got, err := ResolveCampaignSelection("obe", reg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "id-a" {
			t.Fatalf("iteration %d: got %s, want better-scoring id-a", i, got.ID)
		}
	}
}
