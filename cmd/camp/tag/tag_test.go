package tag

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

const tagFixture = `{
  "version": 2,
  "campaigns": {
    "A-1": {"name":"alpha","path":"/tmp/a","type":"campaign","last_access":"2026-06-16T10:00:00Z"},
    "B-2": {"name":"beta","path":"/tmp/b","type":"campaign","last_access":"2026-06-16T10:00:00Z","org":"obey","tags":["paid-work"]}
  }
}`

func setTagRegistry(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", path)
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func execTag(t *testing.T, run func(*cobra.Command, []string) error, asJSON bool, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.Flags().Bool("json", asJSON, "")
	err := run(cmd, args)
	return buf.String(), err
}

func tagsOf(t *testing.T, id string) []string {
	t.Helper()
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	c, ok := reg.Campaigns[id]
	if !ok {
		t.Fatalf("campaign %s missing", id)
	}
	return c.Tags
}

func TestTagAdd_InvalidTag_NoWrite(t *testing.T) {
	path := setTagRegistry(t, tagFixture)
	before, _ := os.ReadFile(path)
	if _, err := execTag(t, runTagAdd, false, "alpha", "Bad-Tag"); err == nil {
		t.Fatal("expected error for invalid tag name")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry modified after invalid tag")
	}
}

func TestTagAdd_UnknownCampaign(t *testing.T) {
	setTagRegistry(t, tagFixture)
	if _, err := execTag(t, runTagAdd, false, "ghost", "foo"); err == nil {
		t.Error("expected error for unknown campaign")
	}
}

func TestTagAdd_SetSemantics(t *testing.T) {
	setTagRegistry(t, tagFixture)
	if _, err := execTag(t, runTagAdd, false, "alpha", "paid-work", "q3-2026"); err != nil {
		t.Fatalf("tag add: %v", err)
	}
	if got, want := tagsOf(t, "A-1"), []string{"paid-work", "q3-2026"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
	out, err := execTag(t, runTagAdd, false, "alpha", "paid-work")
	if err != nil {
		t.Fatalf("tag re-add: %v", err)
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("expected no-op message on re-add, got: %s", out)
	}
	if got, want := tagsOf(t, "A-1"), []string{"paid-work", "q3-2026"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags after re-add = %v, want %v", got, want)
	}
}

func TestTagRm_InvalidTag_NoWrite(t *testing.T) {
	path := setTagRegistry(t, tagFixture)
	before, _ := os.ReadFile(path)
	if _, err := execTag(t, runTagRm, false, "beta", "Bad-Tag"); err == nil {
		t.Fatal("expected error for invalid tag name on rm")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry modified after invalid tag on rm")
	}
}

func TestTagRm_RemovesAndAbsentNoOp(t *testing.T) {
	setTagRegistry(t, tagFixture)
	if _, err := execTag(t, runTagAdd, false, "beta", "q3-2026", "outreach"); err != nil {
		t.Fatalf("setup add: %v", err)
	}
	out, err := execTag(t, runTagRm, false, "beta", "q3-2026", "absent")
	if err != nil {
		t.Fatalf("tag rm: %v", err)
	}
	if !strings.Contains(out, "-q3-2026") {
		t.Errorf("expected q3-2026 removed in output: %s", out)
	}
	got := tagsOf(t, "B-2")
	sort.Strings(got)
	if want := []string{"outreach", "paid-work"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remaining tags = %v, want %v", got, want)
	}
}

func TestTagList_GlobalPoolAcrossOrgs(t *testing.T) {
	setTagRegistry(t, tagFixture)
	if _, err := execTag(t, runTagAdd, false, "alpha", "paid-work"); err != nil {
		t.Fatalf("add: %v", err)
	}
	out, err := execTag(t, runTagList, true)
	if err != nil {
		t.Fatalf("tag list: %v", err)
	}
	var counts []tagCount
	if err := json.Unmarshal([]byte(out), &counts); err != nil {
		t.Fatalf("parse json: %v\n%s", err, out)
	}
	got := make(map[string]int)
	for _, c := range counts {
		got[c.Tag] = c.Campaigns
	}
	if got["paid-work"] != 2 {
		t.Errorf("paid-work count = %d, want 2 (alpha + beta across orgs)", got["paid-work"])
	}
}

func TestMergeTags_DedupeAndSort(t *testing.T) {
	result, added := mergeTags([]string{"b", "a"}, []string{"a", "c"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(result, want) {
		t.Errorf("result = %v, want %v", result, want)
	}
	if want := []string{"c"}; !reflect.DeepEqual(added, want) {
		t.Errorf("added = %v, want %v", added, want)
	}
}
