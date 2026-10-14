package scaffold

import (
	"io/fs"
	"testing"
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
