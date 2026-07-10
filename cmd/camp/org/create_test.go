package org

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
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

func TestOrgCreate_Empty(t *testing.T) {
	// Error path first: --empty rejects campaign args.
	setOrgRegistry(t, orgFixture)
	_, err := execOrgWithFlags(t, runOrgCreate, map[string]bool{"empty": true}, "newco", "alpha")
	if err == nil {
		t.Fatal("expected error when --empty is combined with campaign args")
	}
	if !strings.Contains(err.Error(), "--empty") {
		t.Fatalf("error = %v, want --empty mention", err)
	}

	// Happy path outside any campaign.
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(campaign.EnvCacheDisable, "1")
	setOrgRegistry(t, `{"version":3,"campaigns":{}}`)

	out, err := execOrgWithFlags(t, runOrgCreate, map[string]bool{"empty": true}, "empty-co")
	if err != nil {
		t.Fatalf("create --empty: %v", err)
	}
	if !strings.Contains(out, "created org") {
		t.Fatalf("unexpected output: %s", out)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if !orgExists(reg, "empty-co") {
		t.Fatal("empty-co missing from reg.Orgs")
	}
	if len(membersOf(reg, "empty-co")) != 0 {
		t.Fatalf("empty-co has members: %v", membersOf(reg, "empty-co"))
	}

	// Idempotent second create.
	out, err = execOrgWithFlags(t, runOrgCreate, map[string]bool{"empty": true}, "empty-co")
	if err != nil {
		t.Fatalf("second create --empty: %v", err)
	}
	if !strings.Contains(out, "already exists") {
		t.Fatalf("expected already-exists message, got %s", out)
	}
}

func TestOrgCreate_Empty_JSONShape(t *testing.T) {
	setOrgRegistry(t, `{"version":3,"campaigns":{}}`)
	out, err := execOrgWithFlags(t, runOrgCreate, map[string]bool{"empty": true, "json": true}, "json-org")
	if err != nil {
		t.Fatalf("create --empty --json: %v", err)
	}
	if strings.Contains(out, "null") {
		t.Fatalf("JSON contains null: %s", out)
	}
	var result orgCreateEmptyResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v\nout=%s", err, out)
	}
	if !result.Created || result.Org != "json-org" || result.Members != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOrgCreate_JoinPersistsOrgAfterLastMemberRemoved(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgCreate, false, "newco", "alpha"); err != nil {
		t.Fatalf("create join: %v", err)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !orgExists(reg, "newco") {
		t.Fatal("newco not in reg.Orgs after join create")
	}

	// Remove the only member (return alpha to default).
	if _, err := execOrg(t, runOrgRemove, false, "alpha"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	reg, err = config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load after remove: %v", err)
	}
	if !orgExists(reg, "newco") {
		t.Fatal("newco vanished after last member left; first-class persistence broken")
	}
	if got := orgOf(t, "A-1"); got != "default" {
		t.Fatalf("alpha org = %q, want default", got)
	}
}
