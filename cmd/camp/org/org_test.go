package org

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

const orgFixture = `{
  "version": 2,
  "campaigns": {
    "A-1": {"name":"alpha","path":"/tmp/a","type":"campaign","last_access":"2026-06-16T10:00:00Z"},
    "B-2": {"name":"beta","path":"/tmp/b","type":"campaign","last_access":"2026-06-16T10:00:00Z","org":"obey"},
    "C-3": {"name":"gamma","path":"/tmp/c","type":"campaign","last_access":"2026-06-16T10:00:00Z","org":"obey","status":"inactive","tags":["x"]}
  }
}`

func setOrgRegistry(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func execOrg(t *testing.T, run func(*cobra.Command, []string) error, asJSON bool, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.Flags().Bool("json", asJSON, "")
	err := run(cmd, args)
	return buf.String(), err
}

func orgOf(t *testing.T, id string) string {
	t.Helper()
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	c, ok := reg.Campaigns[id]
	if !ok {
		t.Fatalf("campaign %s missing", id)
	}
	return c.Org
}

func diskKeys(t *testing.T, path string) map[string]map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var raw struct {
		Campaigns map[string]map[string]json.RawMessage `json:"campaigns"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("registry is not valid JSON: %v", err)
	}
	return raw.Campaigns
}

func TestValidateOrgName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"obey", true},
		{"client-acme", true},
		{"a", true},
		{"a1", true},
		{"x-y-z", true},
		{"default", true},
		{"", false},
		{"Bad", false},
		{"UPPER", false},
		{"with space", false},
		{"1abc", false},
		{"-abc", false},
		{"a_b", false},
		{"a/b", false},
		{"a.b", false},
	}
	for _, tc := range cases {
		err := validateOrgName(tc.name)
		if tc.valid && err != nil {
			t.Errorf("validateOrgName(%q) = %v, want nil", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("validateOrgName(%q) = nil, want error", tc.name)
		}
	}
}

func TestOrgAdd_InvalidOrgName_NoWrite(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	before, _ := os.ReadFile(path)

	if _, err := execOrg(t, runOrgAdd, false, "Bad Org", "alpha"); err == nil {
		t.Fatal("expected error for invalid org name")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry was modified after invalid org name")
	}
}

func TestOrgAdd_UnknownCampaign_NoPartialWrite(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	before, _ := os.ReadFile(path)

	if _, err := execOrg(t, runOrgAdd, false, "obey", "alpha", "ghost"); err == nil {
		t.Fatal("expected error for unknown campaign")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry was partially written despite unknown campaign")
	}
	if got := orgOf(t, "A-1"); got != "default" {
		t.Errorf("alpha org = %q, want default (no partial write)", got)
	}
}

func TestOrgRename_UnknownOrEmptyOld(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgRename, false, "ghost", "somewhere"); err == nil {
		t.Error("expected error renaming a non-existent org")
	}
	if _, err := execOrg(t, runOrgRename, false, "", "somewhere"); err == nil {
		t.Error("expected error renaming an empty org")
	}
}

func TestOrgAdd_SetsAndDerivesOrg(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgAdd, false, "obey", "alpha"); err != nil {
		t.Fatalf("org add: %v", err)
	}
	if got := orgOf(t, "A-1"); got != "obey" {
		t.Errorf("alpha org = %q, want obey", got)
	}
	out, err := execOrg(t, runOrgList, false)
	if err != nil {
		t.Fatalf("org list: %v", err)
	}
	if !strings.Contains(out, "obey") {
		t.Errorf("org list missing derived org obey:\n%s", out)
	}
}

func TestOrgAdd_Batch(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgAdd, false, "newco", "alpha", "beta"); err != nil {
		t.Fatalf("org add batch: %v", err)
	}
	if orgOf(t, "A-1") != "newco" || orgOf(t, "B-2") != "newco" {
		t.Error("batch add did not assign both campaigns")
	}
}

func TestOrgAdd_ReassignSingleMembership(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgAdd, false, "client", "beta"); err != nil {
		t.Fatalf("org add: %v", err)
	}
	if got := orgOf(t, "B-2"); got != "client" {
		t.Errorf("beta org = %q, want client", got)
	}
	show, err := execOrg(t, runOrgShow, false, "obey")
	if err != nil {
		t.Fatalf("org show obey: %v", err)
	}
	if strings.Contains(show, "beta") {
		t.Error("beta still listed under obey after reassign")
	}
}

func TestOrgAdd_Idempotent(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgAdd, false, "obey", "beta")
	if err != nil {
		t.Fatalf("org add: %v", err)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("expected no-op message, got: %s", out)
	}
	if got := orgOf(t, "B-2"); got != "obey" {
		t.Errorf("beta org = %q, want obey", got)
	}
	if _, ok := diskKeys(t, path)["B-2"]["org"]; !ok {
		t.Error("beta lost its org key after idempotent add")
	}
}

func TestOrgRemove_ReturnsToFallback(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgRemove, false, "beta", "gamma"); err != nil {
		t.Fatalf("org remove: %v", err)
	}
	if orgOf(t, "B-2") != "default" || orgOf(t, "C-3") != "default" {
		t.Error("remove did not return campaigns to default")
	}
	for _, id := range []string{"B-2", "C-3"} {
		if _, ok := diskKeys(t, path)[id]["org"]; ok {
			t.Errorf("%s still has an org key after returning to default", id)
		}
	}
}

func TestOrgRemove_AlreadyDefault_NoOp(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgRemove, false, "alpha")
	if err != nil {
		t.Fatalf("org remove: %v", err)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("expected no-op for already-default campaign, got: %s", out)
	}
}

func TestOrgRename_ReassignsAllMembers(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgRename, false, "obey", "obedience")
	if err != nil {
		t.Fatalf("org rename: %v", err)
	}
	if !strings.Contains(out, "2 campaigns reassigned") {
		t.Errorf("unexpected rename output: %s", out)
	}
	if orgOf(t, "B-2") != "obedience" || orgOf(t, "C-3") != "obedience" {
		t.Error("rename did not reassign all members")
	}
}

func TestOrgRename_CollisionRejected_NoWrite(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgAdd, false, "client", "alpha"); err != nil {
		t.Fatalf("setup add: %v", err)
	}
	before, _ := os.ReadFile(path)
	if _, err := execOrg(t, runOrgRename, false, "client", "obey"); err == nil {
		t.Fatal("expected collision error renaming client -> obey")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry changed despite rename collision")
	}
}

func TestOrgList_CountsAndActive(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgList, true)
	if err != nil {
		t.Fatalf("org list --json: %v", err)
	}
	var counts []orgCount
	if err := json.Unmarshal([]byte(out), &counts); err != nil {
		t.Fatalf("parse json: %v\n%s", err, out)
	}
	got := make(map[string]orgCount)
	for _, c := range counts {
		got[c.Org] = c
	}
	if got["obey"].Campaigns != 2 || got["obey"].Active != 1 {
		t.Errorf("obey counts = %+v, want campaigns=2 active=1", got["obey"])
	}
	if got["default"].Campaigns != 1 || got["default"].Active != 1 {
		t.Errorf("default counts = %+v, want campaigns=1 active=1", got["default"])
	}
}

func TestOrgShow_MembersAndUnknown(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	out, err := execOrg(t, runOrgShow, false, "obey")
	if err != nil {
		t.Fatalf("org show: %v", err)
	}
	for _, want := range []string{"beta", "gamma", "inactive", "x"} {
		if !strings.Contains(out, want) {
			t.Errorf("org show missing %q:\n%s", want, out)
		}
	}
	if _, err := execOrg(t, runOrgShow, false, "nope"); err == nil {
		t.Error("expected non-zero error for unknown org")
	}
}

func TestOrgBare_PrintsCurrentOrg(t *testing.T) {
	root := makeOrgTestCampaign(t, "B-2")
	t.Chdir(root)
	t.Setenv(campaign.EnvCacheDisable, "1")
	setOrgRegistry(t, orgFixture)

	out, err := execOrg(t, runOrgBare, false)
	if err != nil {
		t.Fatalf("bare org: %v", err)
	}
	if strings.TrimSpace(out) != "obey" {
		t.Errorf("bare org = %q, want obey", strings.TrimSpace(out))
	}

	jsonOut, err := execOrg(t, runOrgBare, true)
	if err != nil {
		t.Fatalf("bare org --json: %v", err)
	}
	var shape struct {
		Campaign string `json:"campaign"`
		Org      string `json:"org"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &shape); err != nil {
		t.Fatalf("parse json: %v\n%s", err, jsonOut)
	}
	if shape.Campaign != "beta" || shape.Org != "obey" {
		t.Errorf("bare org json = %+v, want campaign=beta org=obey", shape)
	}
}

func TestOrgRename_DefaultMovesFallback(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	if _, err := execOrg(t, runOrgRename, false, "default", "personal"); err != nil {
		t.Fatalf("rename default: %v", err)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reg.FallbackOrg() != "personal" {
		t.Errorf("fallback = %q, want personal", reg.FallbackOrg())
	}
	if got := reg.Campaigns["A-1"].Org; got != "personal" {
		t.Errorf("former-default campaign org = %q, want personal", got)
	}
	if _, ok := diskKeys(t, path)["A-1"]["org"]; ok {
		t.Error("fallback member should stay key-free on disk")
	}
}

func TestOrgWrites_ConcurrentStayValid(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	specs := []struct{ org, campaign string }{
		{"one", "alpha"},
		{"two", "beta"},
	}
	wg.Add(2)
	for i, s := range specs {
		i, s := i, s
		go func() {
			defer wg.Done()
			_, errs[i] = execOrg(t, runOrgAdd, false, s.org, s.campaign)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent writer %d error: %v", i, err)
		}
	}
	if orgOf(t, "A-1") != "one" {
		t.Errorf("alpha org = %q, want one", orgOf(t, "A-1"))
	}
	if orgOf(t, "B-2") != "two" {
		t.Errorf("beta org = %q, want two", orgOf(t, "B-2"))
	}
}

func makeOrgTestCampaign(t *testing.T, id string) string {
	t.Helper()
	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0o755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}
	content := "id: " + id + "\nname: test-campaign\n"
	if err := os.WriteFile(filepath.Join(campaignDir, "campaign.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write campaign.yaml: %v", err)
	}
	return root
}
