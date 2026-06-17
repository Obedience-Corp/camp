package festivals

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

const fakeFestSuccess = `#!/bin/sh
cat <<'JSON'
{"active":[{"name":"f1","status":"active","path":"/p/f1","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z","tasks":{"completed":3,"total":5}}],"planning":[{"name":"f2","status":"planning","path":"/p/f2","tasks":{"completed":1,"total":4}}],"total":2}
JSON
`

const fakeFestFail = `#!/bin/sh
echo "boom: not a fest workspace" >&2
exit 1
`

func writeFakeFest(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fest")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake fest: %v", err)
	}
	return path
}

func campaignWithFestivals(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "festivals"), 0o755); err != nil {
		t.Fatalf("mkdir festivals: %v", err)
	}
	return dir
}

func registryWith(campaigns ...config.RegisteredCampaign) *config.Registry {
	reg := config.NewRegistry()
	for _, c := range campaigns {
		reg.Campaigns[c.ID] = c
	}
	return reg
}

func camp(id, name, org, status string, tags ...string) config.RegisteredCampaign {
	if tags == nil {
		tags = []string{}
	}
	return config.RegisteredCampaign{ID: id, Name: name, Org: org, Status: status, Tags: tags}
}

func TestSelectCampaigns_OrgTagAndStatus(t *testing.T) {
	reg := registryWith(
		camp("A", "alpha", "obey", "active", "paid"),
		camp("B", "beta", "obey", "active"),
		camp("C", "gamma", "acme", "active", "paid"),
		camp("D", "delta", "obey", "inactive", "paid"),
	)

	got := names(selectCampaigns(reg, "obey", []string{"paid"}, false))
	if want := []string{"alpha"}; !equal(got, want) {
		t.Errorf("org+tag+active = %v, want %v", got, want)
	}

	gotAll := names(selectCampaigns(reg, "obey", []string{"paid"}, true))
	if want := []string{"alpha", "delta"}; !equal(gotAll, want) {
		t.Errorf("--all-campaigns = %v, want %v", gotAll, want)
	}

	gotOrg := names(selectCampaigns(reg, "acme", nil, false))
	if want := []string{"gamma"}; !equal(gotOrg, want) {
		t.Errorf("org acme = %v, want %v", gotOrg, want)
	}
}

func TestSelectCampaigns_NoSilentCap(t *testing.T) {
	var cs []config.RegisteredCampaign
	for i := 0; i < 50; i++ {
		cs = append(cs, camp("ID"+strconv.Itoa(i), "c"+strconv.Itoa(i), "obey", "active"))
	}
	reg := registryWith(cs...)
	if got := len(selectCampaigns(reg, "obey", nil, false)); got != 50 {
		t.Errorf("selected %d, want 50 (campaign set must not be silently capped)", got)
	}
}

func TestParseFestListJSON_SkipsScalarTotal(t *testing.T) {
	data := []byte(`{"active":[{"name":"f1","status":"active","tasks":{"completed":2,"total":4}}],"total":1}`)
	entries, err := parseFestListJSON(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry (total scalar skipped), got %d", len(entries))
	}
	if entries[0].Name != "f1" || entries[0].Tasks.Completed != 2 || entries[0].Tasks.Total != 4 {
		t.Errorf("bad entry: %+v", entries[0])
	}
}

func TestPassthroughFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("status", "active", "")
	cmd.Flags().String("since", "2026-01-01", "")
	cmd.Flags().String("until", "", "")
	cmd.Flags().String("sort", "name", "")
	cmd.Flags().Bool("all", true, "")

	got := passthroughFlags(cmd)
	joined := strings.Join(got, " ")
	for _, want := range []string{"--status active", "--since 2026-01-01", "--sort name", "--all"} {
		if !strings.Contains(joined, want) {
			t.Errorf("passthrough %q missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "--until") {
		t.Errorf("empty --until should not be forwarded: %q", joined)
	}
}

func TestAggregate_ComposesAndAnnotates(t *testing.T) {
	festPath := writeFakeFest(t, fakeFestSuccess)
	campPath := campaignWithFestivals(t)
	campaigns := []config.RegisteredCampaign{camp("X", "alpha", "obey", "active")}
	campaigns[0].Path = campPath

	items, err := aggregate(context.Background(), festPath, campaigns, nil)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	var f1 *festivalItem
	for i := range items {
		if items[i].Festival == "f1" {
			f1 = &items[i]
		}
	}
	if f1 == nil {
		t.Fatal("f1 missing")
	}
	if f1.Campaign != "alpha" || f1.Org != "obey" || f1.Progress.Completed != 3 || f1.Progress.Total != 5 {
		t.Errorf("bad annotation: %+v", *f1)
	}
}

func TestAggregate_NoWorkspaceContributesNothing(t *testing.T) {
	festPath := writeFakeFest(t, fakeFestFail)
	campaigns := []config.RegisteredCampaign{camp("X", "alpha", "obey", "active")}
	campaigns[0].Path = t.TempDir()

	items, err := aggregate(context.Background(), festPath, campaigns, nil)
	if err != nil {
		t.Fatalf("a campaign without festivals/ must not error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("want 0 items, got %d", len(items))
	}
}

func TestAggregate_FestFailureNamesCampaign(t *testing.T) {
	festPath := writeFakeFest(t, fakeFestFail)
	campPath := campaignWithFestivals(t)
	campaigns := []config.RegisteredCampaign{camp("X", "alpha", "obey", "active")}
	campaigns[0].Path = campPath

	_, err := aggregate(context.Background(), festPath, campaigns, nil)
	if err == nil {
		t.Fatal("expected error when fest fails")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error must name the campaign: %v", err)
	}
}

func TestRenderJSON_Shape(t *testing.T) {
	out := festivalsOutput{
		SchemaVersion: schemaVersion,
		Filter:        festivalsFilter{Org: "obey", Tags: []string{}},
		Items: []festivalItem{
			{Campaign: "alpha", Org: "obey", Festival: "f1", Status: "active", Progress: progress{Completed: 3, Total: 5}},
		},
	}
	var buf bytes.Buffer
	if err := encodeJSON(&buf, out); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got festivalsOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SchemaVersion != "camp-festivals/v1" {
		t.Errorf("schema_version = %q", got.SchemaVersion)
	}
	if got.Filter.Org != "obey" || got.Filter.Tags == nil {
		t.Errorf("filter = %+v (tags must be non-null)", got.Filter)
	}
	if len(got.Items) != 1 || got.Items[0].Progress.Total != 5 {
		t.Errorf("items = %+v", got.Items)
	}
}

func TestRenderHuman_GroupedFormat(t *testing.T) {
	items := []festivalItem{
		{Campaign: "obey-campaign", Org: "obey", Festival: "f1", Status: "active", Progress: progress{Completed: 3, Total: 5}},
		{Campaign: "alpha", Org: "default", Festival: "f0", Status: "planning", Progress: progress{Completed: 0, Total: 2}},
	}
	var buf bytes.Buffer
	if err := renderFestivalsHuman(&buf, items, "default"); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ACTIVE") || !strings.Contains(out, "3/5") || !strings.Contains(out, "f1") {
		t.Errorf("missing festival row:\n%s", out)
	}
	if strings.Index(out, "default") > strings.Index(out, "obey\n") {
		t.Errorf("fallback org 'default' must render before 'obey':\n%s", out)
	}
}

func names(cs []config.RegisteredCampaign) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
