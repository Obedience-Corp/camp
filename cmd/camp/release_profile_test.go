package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/commands/release"
)

func assertGendocsCommand(t *testing.T) {
	t.Helper()

	cmd, _, err := rootCmd.Find([]string{"gendocs"})
	if err != nil {
		t.Fatalf("expected gendocs command: %v", err)
	}
	if cmd == nil || cmd.Name() != "gendocs" {
		t.Fatalf("expected gendocs command, got %#v", cmd)
	}
	if !cmd.Hidden {
		t.Fatal("gendocs should be hidden from the normal help surface")
	}
	if got := cmd.Annotations[release.AnnotationReleaseChannel]; got != "" {
		t.Fatalf("gendocs release_channel = %q, want empty", got)
	}
}

func TestRunGendocs_RemovesStaleFilesAndSkipsHiddenCommands(t *testing.T) {
	dir := t.TempDir()

	staleFile := filepath.Join(dir, "camp_fakecmd.md")
	if err := os.WriteFile(staleFile, []byte("# stale"), 0644); err != nil {
		t.Fatal(err)
	}

	keepFile := filepath.Join(dir, "custom-notes.md")
	if err := os.WriteFile(keepFile, []byte("# keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	gendocsOutput = dir
	gendocsFormat = "markdown"
	gendocsSingle = false

	if err := runGendocs(rootCmd, nil); err != nil {
		t.Fatalf("runGendocs: %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale file %s still exists after gendocs", filepath.Base(staleFile))
	}

	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("non-generated file %s was removed: %v", filepath.Base(keepFile), err)
	}

	if _, err := os.Stat(filepath.Join(dir, "camp_gendocs.md")); !os.IsNotExist(err) {
		t.Errorf("hidden gendocs command should not have generated docs, err=%v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var generated []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "camp") && strings.HasSuffix(entry.Name(), ".md") {
			generated = append(generated, entry.Name())
		}
	}
	if len(generated) == 0 {
		t.Fatal("expected at least one generated camp*.md file")
	}
}
