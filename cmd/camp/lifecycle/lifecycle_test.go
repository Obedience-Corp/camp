package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

const lifecycleFixture = `{
  "version": 2,
  "campaigns": {
    "A-1": {"name":"alpha","path":"/tmp/a","type":"campaign","last_access":"2026-06-16T10:00:00Z"},
    "B-2": {"name":"beta","path":"/tmp/b","type":"campaign","last_access":"2026-06-16T10:00:00Z","status":"inactive"}
  }
}`

func setLifecycleRegistry(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func execLifecycle(t *testing.T, run func(*cobra.Command, []string) error, asJSON bool, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.Flags().Bool("json", asJSON, "")
	err := run(cmd, args)
	return buf.String(), err
}

func statusOf(t *testing.T, id string) string {
	t.Helper()
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	c, ok := reg.Campaigns[id]
	if !ok {
		t.Fatalf("campaign %s missing", id)
	}
	return c.Status
}

func TestLifecycleSet_InvalidValue_NoWrite(t *testing.T) {
	path := setLifecycleRegistry(t, lifecycleFixture)
	before, _ := os.ReadFile(path)
	_, err := execLifecycle(t, runLifecycleSet, false, "alpha", "paused")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	if !strings.Contains(err.Error(), "active, inactive, reference") {
		t.Errorf("error missing allowed-values list: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry modified after invalid status")
	}
}

func TestValidateStatus(t *testing.T) {
	for _, s := range []string{"active", "inactive", "reference"} {
		if err := config.ValidateStatus(s); err != nil {
			t.Errorf("ValidateStatus(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"paused", "Active", "", "archived", "ACTIVE"} {
		if err := config.ValidateStatus(s); err == nil {
			t.Errorf("ValidateStatus(%q) = nil, want error", s)
		}
	}
}

func TestLifecycleSet_TransitionsAndPersists(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	out, err := execLifecycle(t, runLifecycleSet, false, "alpha", "reference")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(out, "active -> reference") {
		t.Errorf("unexpected output: %s", out)
	}
	if got := statusOf(t, "A-1"); got != "reference" {
		t.Errorf("alpha status = %q, want reference", got)
	}
}

func TestLifecycleSet_DoesNotUnregister(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	if _, err := execLifecycle(t, runLifecycleSet, false, "alpha", "inactive"); err != nil {
		t.Fatalf("set: %v", err)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, ok := reg.Campaigns["A-1"]; !ok {
		t.Error("campaign was unregistered after lifecycle set")
	}
}

func TestLifecycleSet_UnknownCampaign(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	if _, err := execLifecycle(t, runLifecycleSet, false, "ghost", "active"); err == nil {
		t.Error("expected error for unknown campaign")
	}
}

func TestLifecycleList_CountsPerStatus(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	out, err := execLifecycle(t, runLifecycleList, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var counts []statusCount
	if err := json.Unmarshal([]byte(out), &counts); err != nil {
		t.Fatalf("parse json: %v\n%s", err, out)
	}
	got := make(map[string]int)
	for _, c := range counts {
		got[c.Status] = c.Campaigns
	}
	if got["active"] != 1 || got["inactive"] != 1 || got["reference"] != 0 {
		t.Errorf("counts = %v, want active=1 inactive=1 reference=0", got)
	}
}

func TestLifecycleSet_NoOpWhenAlreadySet(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	out, err := execLifecycle(t, runLifecycleSet, false, "beta", "inactive")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(out, "already inactive") {
		t.Errorf("expected no-op message, got: %s", out)
	}
}

func TestLifecycleList_TextOutput(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	out, err := execLifecycle(t, runLifecycleList, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"STATUS", "active", "inactive", "reference"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestLifecycle_StatusDefaultsToActiveOnLoad(t *testing.T) {
	setLifecycleRegistry(t, lifecycleFixture)
	if got := statusOf(t, "A-1"); got != "active" {
		t.Errorf("alpha (no status key) loaded as %q, want active", got)
	}
}
