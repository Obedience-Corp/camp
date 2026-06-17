package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
