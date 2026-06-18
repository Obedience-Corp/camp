package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

func TestListCountFlagRegistered(t *testing.T) {
	if listCmd.Flags().Lookup("count") == nil {
		t.Fatal("camp list missing --count flag")
	}
}

func TestOutputCampaigns_TableShowsCount(t *testing.T) {
	campaigns := []campaignEntry{
		{ID: "abc12345", Name: "alpha", Type: "work", Path: "/tmp/alpha"},
		{ID: "def67890", Name: "beta", Type: "work", Path: "/tmp/beta"},
	}

	var buf bytes.Buffer
	if err := outputCampaigns(&buf, campaigns, "table"); err != nil {
		t.Fatalf("outputCampaigns() error = %v", err)
	}

	if !strings.Contains(buf.String(), "2 campaigns") {
		t.Errorf("table output missing count footer; got:\n%s", buf.String())
	}
}

func TestOutputCampaigns_SimpleHasNoCount(t *testing.T) {
	campaigns := []campaignEntry{
		{ID: "abc12345", Name: "alpha"},
		{ID: "def67890", Name: "beta"},
	}

	var buf bytes.Buffer
	if err := outputCampaigns(&buf, campaigns, "simple"); err != nil {
		t.Fatalf("outputCampaigns() error = %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if strings.Contains(out, "campaigns") {
		t.Errorf("simple output should not include count footer; got:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); len(lines) != 2 {
		t.Errorf("simple output should have 2 lines, got %d", len(lines))
	}
}

func TestOutputCampaigns_JSONHasNoCount(t *testing.T) {
	campaigns := []campaignEntry{{ID: "abc12345", Name: "alpha"}}

	var buf bytes.Buffer
	if err := outputCampaigns(&buf, campaigns, "json"); err != nil {
		t.Fatalf("outputCampaigns() error = %v", err)
	}

	var parsed []campaignEntry
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON output is no longer a bare array: %v", err)
	}
	if len(parsed) != 1 {
		t.Errorf("JSON has %d campaigns, want 1", len(parsed))
	}
}

type listCountSpec struct {
	id, name, org, status string
}

func setupListCountRegistry(t *testing.T, specs []listCountSpec) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	reg := config.NewRegistry()
	for _, s := range specs {
		dir := filepath.Join(base, s.name)
		cfg := &config.CampaignConfig{ID: s.id, Name: s.name, Type: config.CampaignTypeProduct}
		if err := config.SaveCampaignConfig(ctx, dir, cfg); err != nil {
			t.Fatalf("SaveCampaignConfig %s: %v", s.name, err)
		}
		if err := reg.Register(s.id, s.name, dir, config.CampaignTypeProduct); err != nil {
			t.Fatalf("Register %s: %v", s.name, err)
		}
		c := reg.Campaigns[s.id]
		c.Org = s.org
		c.Status = s.status
		reg.Campaigns[s.id] = c
	}
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(base, "registry.json"))
	if err := config.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
}

func listCountCmd(format, org, status string, all bool) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().String("format", format, "")
	cmd.Flags().String("sort", "accessed", "")
	cmd.Flags().String("org", org, "")
	cmd.Flags().StringSlice("tag", nil, "")
	cmd.Flags().String("status", status, "")
	cmd.Flags().Bool("all", all, "")
	cmd.Flags().Bool("group", false, "")
	cmd.Flags().Bool("no-group", false, "")
	cmd.Flags().Bool("verify-verbose", false, "")
	return cmd
}

func countListSpecs() []listCountSpec {
	return []listCountSpec{
		{"count-alpha", "alpha", config.DefaultOrg, config.StatusActive},
		{"count-beta", "beta", "obey", config.StatusActive},
		{"count-gamma", "gamma", config.DefaultOrg, config.StatusInactive},
		{"count-delta", "delta", config.DefaultOrg, config.StatusReference},
	}
}

func TestListCount_TextRespectsFilters(t *testing.T) {
	prevCount, prevJSON := listCount, listJSON
	t.Cleanup(func() { listCount, listJSON = prevCount, prevJSON })
	listJSON = false

	cases := []struct {
		name string
		cmd  *cobra.Command
		want string
	}{
		{"default hides non-active", listCountCmd("table", "", "", false), "2 campaigns"},
		{"--all counts every status", listCountCmd("table", "", "", true), "4 campaigns"},
		{"--org filters the count", listCountCmd("table", "obey", "", false), "1 campaign"},
		{"--status reference", listCountCmd("table", "", "reference", false), "1 campaign"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupListCountRegistry(t, countListSpecs())
			listCount = true
			out := captureListStdout(t, func() error { return runList(tc.cmd, nil) })
			if strings.TrimSpace(out) != tc.want {
				t.Errorf("--count output = %q, want %q (must reflect filters, not the 4-entry registry total)", strings.TrimSpace(out), tc.want)
			}
		})
	}
}

func TestListCount_JSONRespectsFilters(t *testing.T) {
	prevCount, prevJSON := listCount, listJSON
	t.Cleanup(func() { listCount, listJSON = prevCount, prevJSON })

	cases := []struct {
		name string
		cmd  *cobra.Command
		want int
	}{
		{"default active-only", listCountCmd("json", "", "", false), 2},
		{"--org obey", listCountCmd("json", "obey", "", false), 1},
		{"--all", listCountCmd("json", "", "", true), 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupListCountRegistry(t, countListSpecs())
			listCount, listJSON = true, true
			out := captureListStdout(t, func() error { return runList(tc.cmd, nil) })
			var got map[string]int
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("json: %v\n%s", err, out)
			}
			if got["count"] != tc.want {
				t.Errorf("--count --json = %d, want %d (filtered count, not reg.Len())", got["count"], tc.want)
			}
		})
	}
}
