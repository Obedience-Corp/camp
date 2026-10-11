//go:build container_fs

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExploreExecuteDoesNotWriteConfig(t *testing.T) {
	t.Setenv("CAMP_ROOT", "")
	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.Mkdir(campaignDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(campaignDir, "campaign.yaml")
	original := []byte("name: fixture\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, root)

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(nil)
	oldArgs := os.Args
	os.Args = []string{"camp", "dungeon", "explore", "--json"}
	t.Cleanup(func() {
		os.Args = oldArgs
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		cmd, _, err := rootCmd.Find([]string{"dungeon", "explore"})
		if err != nil {
			return
		}
		cmd.SetOut(nil)
		cmd.SetErr(nil)
		if f := cmd.Flags().Lookup("json"); f != nil {
			_ = cmd.Flags().Set("json", "false")
			f.Changed = false
		}
	})

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v\n%s%s", err, stdout.String(), stderr.String())
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("campaign.yaml changed:\n%s", got)
	}
	jumps := filepath.Join(campaignDir, "settings", "jumps.yaml")
	if _, err := os.Stat(jumps); !os.IsNotExist(err) {
		t.Fatalf("explore created jumps.yaml: %v", err)
	}
	if !strings.Contains(stdout.String(), "camp-dungeon-explore/v1") {
		t.Fatalf("stdout = %s\nstderr = %s", stdout.String(), stderr.String())
	}
}
