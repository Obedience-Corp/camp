package org

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

func TestOrgCreate_WithCampaignArgs(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgCreate, false, "newco", "alpha"); err != nil {
		t.Fatalf("org create: %v", err)
	}
	if got := orgOf(t, "A-1"); got != "newco" {
		t.Errorf("alpha org = %q, want newco", got)
	}
}

func TestOrgCreate_ExistingOrg_Idempotent(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgCreate, false, "obey", "alpha"); err != nil {
		t.Fatalf("org create into existing org: %v", err)
	}
	if got := orgOf(t, "A-1"); got != "obey" {
		t.Errorf("alpha org = %q, want obey", got)
	}
}

func TestOrgCreate_AlreadyMember_NoOp(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgCreate, false, "obey", "beta")
	if err != nil {
		t.Fatalf("org create: %v", err)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("expected no-op message, got: %s", out)
	}
}

func TestOrgCreate_InvalidOrgName_NoWrite(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	before, _ := os.ReadFile(path)
	if _, err := execOrg(t, runOrgCreate, false, "Bad Org", "alpha"); err == nil {
		t.Fatal("expected error for invalid org name")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry modified after invalid org name")
	}
}

func TestOrgCreate_CurrentCampaign_NoArgs(t *testing.T) {
	root := makeOrgTestCampaign(t, "A-1")
	t.Chdir(root)
	t.Setenv(campaign.EnvCacheDisable, "1")
	setOrgRegistry(t, orgFixture)

	if _, err := execOrg(t, runOrgCreate, false, "obey"); err != nil {
		t.Fatalf("org create (current campaign): %v", err)
	}
	if got := orgOf(t, "A-1"); got != "obey" {
		t.Errorf("current campaign org = %q, want obey", got)
	}
}

func TestOrgCreate_NotInCampaign_Errors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(campaign.EnvCacheDisable, "1")
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgCreate, false, "obey"); err == nil {
		t.Error("expected error: no campaign args outside a campaign")
	}
}

func TestOrgCreate_UnregisteredCurrent_Errors(t *testing.T) {
	root := makeOrgTestCampaign(t, "ZZZ-unregistered")
	t.Chdir(root)
	t.Setenv(campaign.EnvCacheDisable, "1")
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgCreate, false, "obey"); err == nil {
		t.Error("expected error: current campaign not registered")
	}
}
