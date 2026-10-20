package scaffold

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
)

func TestEmbeddedScaffoldFSAcceptsWindowsTemplatePaths(t *testing.T) {
	const path = `campaign\templates\.campaign\intents\OBEY.md.tmpl`
	fsy := embeddedScaffoldFS()
	data, err := fs.ReadFile(fsy, path)
	if err != nil {
		t.Fatalf("read Windows-style embedded template path: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded template is empty")
	}
	if _, err := fs.Stat(fsy, path); err != nil {
		t.Fatalf("stat Windows-style embedded template path: %v", err)
	}
}

func TestWithoutFileFSWithholdsOnlyTheDefaultQuestTemplate(t *testing.T) {
	base := embeddedScaffoldFS()
	if _, err := fs.Stat(base, defaultQuestTemplatePath); err != nil {
		t.Fatalf("default quest template %s is not embedded: %v", defaultQuestTemplatePath, err)
	}
	filtered := withoutFileFS{FS: base, hidden: defaultQuestTemplatePath}

	for _, name := range []string{defaultQuestTemplatePath, strings.ReplaceAll(defaultQuestTemplatePath, "/", `\`)} {
		if _, err := filtered.Open(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%q) error = %v, want fs.ErrNotExist", name, err)
		}
	}

	const defaultQuestOutput = ".campaign/quests/default/quest.yaml"
	const questRootKeep = ".campaign/quests/.gitkeep"
	baseOutputs := scannedOutputs(t, base)
	filteredOutputs := scannedOutputs(t, filtered)
	if !baseOutputs[defaultQuestOutput] {
		t.Fatalf("unfiltered scan is missing %s", defaultQuestOutput)
	}
	if filteredOutputs[defaultQuestOutput] {
		t.Fatalf("filtered scan still renders %s", defaultQuestOutput)
	}
	if !filteredOutputs[questRootKeep] {
		t.Fatalf("filtered scan dropped %s", questRootKeep)
	}
	if len(baseOutputs)-len(filteredOutputs) != 1 {
		t.Fatalf("filtered scan has %d outputs, want %d", len(filteredOutputs), len(baseOutputs)-1)
	}
}

func scannedOutputs(t *testing.T, fsys fs.FS) map[string]bool {
	t.Helper()
	entries, err := scaffold.ScanTemplatesDir(fsys, "campaign/templates")
	if err != nil {
		t.Fatalf("ScanTemplatesDir() error = %v", err)
	}
	outputs := make(map[string]bool, len(entries))
	for _, entry := range entries {
		outputs[filepath.ToSlash(entry.Path)] = true
	}
	return outputs
}
