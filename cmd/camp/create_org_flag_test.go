package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

func TestCreateOrgFlag_InvalidNameNoRegistryWrite(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", regPath)
	_ = os.WriteFile(regPath, []byte(`{"version":3,"campaigns":{}}`), 0o644)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cmd := &cobra.Command{Use: "create", RunE: runCreate}
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().StringP("type", "t", "product", "")
	cmd.Flags().StringP("description", "d", "", "")
	cmd.Flags().StringP("mission", "m", "", "")
	cmd.Flags().Bool("no-git", false, "")
	cmd.Flags().Bool("no-skills", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("path", "", "")
	cmd.Flags().String("org", "", "")
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	_ = cmd.Flags().Set("org", "Bad Name")
	_ = cmd.Flags().Set("path", dir)
	_ = cmd.Flags().Set("description", "d")
	_ = cmd.Flags().Set("mission", "m")
	_ = cmd.Flags().Set("no-git", "true")
	_ = cmd.Flags().Set("no-skills", "true")

	err := cmd.RunE(cmd, []string{"demo-bad"})
	if err == nil {
		t.Fatal("expected validation error for bad org name")
	}
	reg, loadErr := config.LoadRegistry(context.Background())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(reg.Campaigns) != 0 {
		t.Fatalf("registry should be empty after invalid --org, got %d campaigns", len(reg.Campaigns))
	}
}

func TestCreateOrgFlag_DryRunMentionsOrg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Capture stdout from dry-run path (create prints to writers using stdout)
	// Use dry-run with --org; should not write registry.
	cmd := createCmd
	// createCmd is shared; set flags carefully
	_ = cmd.Flags().Set("org", "obey")
	_ = cmd.Flags().Set("path", dir)
	_ = cmd.Flags().Set("description", "d")
	_ = cmd.Flags().Set("mission", "m")
	_ = cmd.Flags().Set("dry-run", "true")
	_ = cmd.Flags().Set("no-git", "true")
	_ = cmd.Flags().Set("no-skills", "true")
	cmd.SetContext(context.Background())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runCreate(cmd, []string{"demo-dry"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	// reset flags for other tests
	_ = cmd.Flags().Set("org", "")
	_ = cmd.Flags().Set("dry-run", "false")
	_ = cmd.Flags().Set("path", "")

	if err != nil {
		t.Fatalf("dry-run create: %v\nout=%s", err, out)
	}
	if !strings.Contains(out, "obey") {
		t.Fatalf("dry-run output missing org: %s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "registry.json")); !os.IsNotExist(statErr) {
		// registry may not exist; if it does, campaigns must stay empty
		reg, _ := config.LoadRegistry(context.Background())
		if reg != nil && len(reg.Campaigns) != 0 {
			t.Fatalf("dry-run wrote campaigns: %d", len(reg.Campaigns))
		}
	}
}
