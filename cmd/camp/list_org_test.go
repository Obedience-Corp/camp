package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

func ent(id, name, typ, org, status string, tags ...string) campaignEntry {
	if tags == nil {
		tags = []string{}
	}
	return campaignEntry{ID: id, Name: name, Type: typ, Path: "/tmp/" + name, Org: org, Status: status, Tags: tags}
}

func names(entries []campaignEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func captureListStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	ferr := fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if ferr != nil {
		t.Fatalf("render error: %v", ferr)
	}
	return buf.String()
}

func TestFilterEntries(t *testing.T) {
	all := []campaignEntry{
		ent("A-1", "alpha", "campaign", "default", "active"),
		ent("B-2", "beta", "campaign", "obey", "active", "paid-work"),
		ent("C-3", "gamma", "campaign", "obey", "inactive", "paid-work", "q3"),
		ent("D-4", "delta", "campaign", "default", "reference"),
	}
	cases := []struct {
		name   string
		filter listFilter
		want   []string
	}{
		{"default hides non-active", listFilter{}, []string{"alpha", "beta"}},
		{"--all shows all", listFilter{all: true}, []string{"alpha", "beta", "gamma", "delta"}},
		{"--status inactive", listFilter{status: "inactive"}, []string{"gamma"}},
		{"--status reference", listFilter{status: "reference"}, []string{"delta"}},
		{"--org obey (active default)", listFilter{org: "obey"}, []string{"beta"}},
		{"--org obey --all", listFilter{org: "obey", all: true}, []string{"beta", "gamma"}},
		{"--tag AND", listFilter{tags: []string{"paid-work", "q3"}, all: true}, []string{"gamma"}},
		{"--org+--tag+--status compose", listFilter{org: "obey", tags: []string{"paid-work"}, status: "inactive"}, []string{"gamma"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(filterEntries(all, tc.filter))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filter = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldGroupAndOrgOrder(t *testing.T) {
	byOrg := map[string][]campaignEntry{
		"obey":    {ent("B-2", "beta", "campaign", "obey", "active")},
		"default": {ent("A-1", "alpha", "campaign", "default", "active")},
		"acme":    {ent("E-5", "eps", "campaign", "acme", "active")},
	}
	got := sortedGroupOrgs(byOrg, "default")
	want := []string{"default", "acme", "obey"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("org order = %v, want %v (fallback first, then alphabetical)", got, want)
	}
}

func TestSortCampaigns_ByOrg(t *testing.T) {
	campaigns := map[string]config.RegisteredCampaign{
		"A-1": {Name: "alpha", Org: "obey", Status: "active"},
		"B-2": {Name: "beta", Org: "default", Status: "active"},
		"C-3": {Name: "gamma", Org: "acme", Status: "active"},
		"D-4": {Name: "delta", Org: "obey", Status: "active"},
	}
	got := names(sortCampaigns(campaigns, "org", "default"))
	want := []string{"beta", "gamma", "alpha", "delta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--sort org = %v, want %v (fallback first, then org alphabetical, then name)", got, want)
	}
}

func TestSortCampaigns_ByOrg_CustomFallback(t *testing.T) {
	campaigns := map[string]config.RegisteredCampaign{
		"A-1": {Name: "alpha", Org: "obey", Status: "active"},
		"B-2": {Name: "beta", Org: "acme", Status: "active"},
	}
	got := names(sortCampaigns(campaigns, "org", "obey"))
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--sort org (fallback=obey) = %v, want %v (custom fallback sorts first)", got, want)
	}
}

func TestSortCampaigns_InvalidSortFallsToAccessed(t *testing.T) {
	now := time.Now()
	campaigns := map[string]config.RegisteredCampaign{
		"A-1": {Name: "old", Org: "default", Status: "active", LastAccess: now.Add(-time.Hour)},
		"B-2": {Name: "new", Org: "default", Status: "active", LastAccess: now},
	}
	got := names(sortCampaigns(campaigns, "bogus", "default"))
	want := []string{"new", "old"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--sort bogus = %v, want %v (falls through to accessed: most recent first)", got, want)
	}
}

func TestList_JSONShape(t *testing.T) {
	ui.SetNoColor(true)
	entries := []campaignEntry{
		ent("A-1", "alpha", "campaign", "default", "active"),
		ent("B-2", "beta", "campaign", "obey", "active", "paid-work"),
	}
	out := captureListStdout(t, func() error { return outputCampaigns(os.Stdout, entries, "json") })

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	for _, obj := range got {
		if _, ok := obj["org"]; !ok {
			t.Errorf("json object missing org: %v", obj)
		}
		if _, ok := obj["status"]; !ok {
			t.Errorf("json object missing status: %v", obj)
		}
		tags, ok := obj["tags"]
		if !ok || tags == nil {
			t.Errorf("json object tags must be present and non-null: %v", obj)
		}
	}
	if got[0]["tags"] == nil {
		t.Error("alpha tags marshaled as null, want []")
	}
}

func TestList_GoldenSingleOrgActive(t *testing.T) {
	ui.SetNoColor(true)
	entries := []campaignEntry{
		ent("AAAAAAAA1111", "alpha", "campaign", "default", "active"),
		ent("BBBBBBBB2222", "beta", "product", "default", "active"),
	}
	if shouldGroupEntries(entries) {
		t.Fatal("single-org entries must not group")
	}
	out := captureListStdout(t, func() error { return outputCampaigns(os.Stdout, entries, "table") })
	const golden = "ID        NAME   ORG      TYPE      PATH\n" +
		"AAAAAAAA  alpha  default  campaign  /tmp/alpha\n" +
		"BBBBBBBB  beta   default  product   /tmp/beta\n" +
		"\n" +
		"2 campaigns\n"
	if out != golden {
		t.Errorf("flat table output drifted:\n--- got ---\n%q\n--- want ---\n%q", out, golden)
	}
}

func TestList_GroupedOutput(t *testing.T) {
	ui.SetNoColor(true)
	entries := []campaignEntry{
		ent("AAAAAAAA1111", "alpha", "campaign", "default", "active"),
		ent("BBBBBBBB2222", "beta", "campaign", "obey", "active"),
	}
	out := captureListStdout(t, func() error { return outputGrouped(entries, "table", "default") })
	const golden = "default\n" +
		"  ID        NAME   TYPE      PATH\n" +
		"  AAAAAAAA  alpha  campaign  /tmp/alpha\n" +
		"\n" +
		"obey\n" +
		"  ID        NAME  TYPE      PATH\n" +
		"  BBBBBBBB  beta  campaign  /tmp/beta\n" +
		"\n" +
		"2 campaigns\n"
	if out != golden {
		t.Errorf("grouped output drifted:\n--- got ---\n%q\n--- want ---\n%q", out, golden)
	}
}

func shouldGroupEntries(entries []campaignEntry) bool {
	return distinctOrgs(entries) > 1
}

func TestList_InvalidStatusRejectedBeforeRegistryWork(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().String("format", "table", "")
	cmd.Flags().String("org", "", "")
	cmd.Flags().StringSlice("tag", nil, "")
	cmd.Flags().String("status", "bogus", "")
	cmd.Flags().Bool("all", false, "")
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(t.TempDir(), "registry.json"))

	if err := runList(cmd, nil); err == nil {
		t.Fatal("expected error: invalid --status must be rejected even on an empty registry")
	}
}

func TestList_FilterGroupPerformance(t *testing.T) {
	const n = 500
	entries := make([]campaignEntry, 0, n)
	for i := 0; i < n; i++ {
		org := "default"
		if i%3 == 0 {
			org = "obey"
		}
		status := "active"
		if i%5 == 0 {
			status = "inactive"
		}
		entries = append(entries, ent("ID"+strconv.Itoa(i), "c"+strconv.Itoa(i), "campaign", org, status, "t"+strconv.Itoa(i%7)))
	}
	start := time.Now()
	filtered := filterEntries(entries, listFilter{all: true, tags: []string{"t1"}})
	byOrg := map[string][]campaignEntry{}
	for _, e := range filtered {
		byOrg[e.Org] = append(byOrg[e.Org], e)
	}
	_ = sortedGroupOrgs(byOrg, "default")
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("filter+group of %d campaigns took %v, want <100ms", n, elapsed)
	}
}
