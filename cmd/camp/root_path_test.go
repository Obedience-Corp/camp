package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

func TestBuildCampaignRootOutput(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		root string
		want string
	}{
		{
			name: "at root",
			cwd:  "/tmp/campaign",
			root: "/tmp/campaign",
			want: ".",
		},
		{
			name: "nested directory",
			cwd:  "/tmp/campaign/projects/camp",
			root: "/tmp/campaign",
			want: "../..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCampaignRootOutput(tt.cwd, tt.root)
			if err != nil {
				t.Fatalf("buildCampaignRootOutput() error = %v", err)
			}
			if got.RelativeRoot != tt.want {
				t.Fatalf("RelativeRoot = %q, want %q", got.RelativeRoot, tt.want)
			}
			if got.CWD != tt.cwd {
				t.Fatalf("CWD = %q, want %q", got.CWD, tt.cwd)
			}
			if got.AbsoluteRoot != tt.root {
				t.Fatalf("AbsoluteRoot = %q, want %q", got.AbsoluteRoot, tt.root)
			}
		})
	}
}

func TestCampaignRootCommand_PrintsRelativeRoot(t *testing.T) {
	root := makeTestCampaign(t, "test-root-123")
	nested := filepath.Join(root, "projects", "camp")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	chdirForTest(t, nested)

	cmd := newRootPathTestCommand(t)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := runCampaignRoot(cmd, nil); err != nil {
		t.Fatalf("runCampaignRoot() error = %v", err)
	}

	if got := strings.TrimSpace(buf.String()); got != "../.." {
		t.Fatalf("runCampaignRoot() output = %q, want %q", got, "../..")
	}
}

func TestCampaignRootCommand_JSON(t *testing.T) {
	root := makeTestCampaign(t, "test-root-json")
	nested := filepath.Join(root, "docs", "notes")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	chdirForTest(t, nested)

	cmd := newRootPathTestCommand(t)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json flag: %v", err)
	}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := runCampaignRoot(cmd, nil); err != nil {
		t.Fatalf("runCampaignRoot() error = %v", err)
	}

	var got campaignRootOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}

	expectedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	expectedRoot, err = filepath.Abs(expectedRoot)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}

	expectedCWD, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatalf("resolve cwd: %v", err)
	}
	expectedCWD, err = filepath.Abs(expectedCWD)
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}

	if got.RelativeRoot != "../.." {
		t.Fatalf("RelativeRoot = %q, want %q", got.RelativeRoot, "../..")
	}
	if got.CWD != expectedCWD {
		t.Fatalf("CWD = %q, want %q", got.CWD, expectedCWD)
	}
	if got.AbsoluteRoot != expectedRoot {
		t.Fatalf("AbsoluteRoot = %q, want %q", got.AbsoluteRoot, expectedRoot)
	}
}

func TestCampaignRootCommand_ReturnsErrorOutsideCampaign(t *testing.T) {
	chdirForTest(t, t.TempDir())

	cmd := newRootPathTestCommand(t)

	err := runCampaignRoot(cmd, nil)
	if err == nil {
		t.Fatal("runCampaignRoot() error = nil, want campaign detection error")
	}
	if !errors.Is(err, campaign.ErrNotInCampaign) {
		t.Fatalf("runCampaignRoot() error = %v, want %v", err, campaign.ErrNotInCampaign)
	}
}

func TestCampaignRootCommand_RegisteredOnRoot(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"root"})
	if err != nil {
		t.Fatalf("rootCmd.Find(root) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "root" {
		t.Fatalf("rootCmd.Find(root) = %#v, want root command", cmd)
	}
	if campaignRootCmd.GroupID != "navigation" {
		t.Fatalf("campaignRootCmd.GroupID = %q, want navigation", campaignRootCmd.GroupID)
	}
}

func newRootPathTestCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().Bool("json", false, "output as JSON")
	return cmd
}
