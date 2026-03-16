package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

func TestIDCommand_PrintsCampaignID(t *testing.T) {
	root := makeTestCampaign(t, "test-id-1234")
	chdirForTest(t, root)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := runID(cmd, nil); err != nil {
		t.Fatalf("runID() error = %v", err)
	}

	if got := strings.TrimSpace(buf.String()); got != "test-id-1234" {
		t.Fatalf("runID() output = %q, want %q", got, "test-id-1234")
	}
}

func TestIDCommand_ReturnsErrorOutsideCampaign(t *testing.T) {
	chdirForTest(t, t.TempDir())

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runID(cmd, nil)
	if err == nil {
		t.Fatal("runID() error = nil, want campaign detection error")
	}
	if !errors.Is(err, campaign.ErrNotInCampaign) {
		t.Fatalf("runID() error = %v, want %v", err, campaign.ErrNotInCampaign)
	}
}

func TestIDCommand_RegisteredOnRoot(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"id"})
	if err != nil {
		t.Fatalf("rootCmd.Find(id) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "id" {
		t.Fatalf("rootCmd.Find(id) = %#v, want id command", cmd)
	}
	if idCmd.GroupID != "campaign" {
		t.Fatalf("idCmd.GroupID = %q, want campaign", idCmd.GroupID)
	}
}

func makeTestCampaign(t *testing.T, id string) string {
	t.Helper()

	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}

	content := "id: " + id + "\nname: test-campaign\n"
	if err := os.WriteFile(filepath.Join(campaignDir, "campaign.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write campaign.yaml: %v", err)
	}

	return root
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	campaign.ClearCache()

	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
		campaign.ClearCache()
	})
}
