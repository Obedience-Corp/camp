package workitem

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestSelectedJumpPathUsesCampaignRelativeDirectoryPath(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: "workflow/design/example",
		ItemKind:     wkitem.ItemKindDirectory,
	}

	got := selectedJumpPath(item)
	if got != item.RelativePath {
		t.Fatalf("selectedJumpPath() = %q, want %q", got, item.RelativePath)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("selectedJumpPath() returned absolute path: %q", got)
	}
}

func TestSelectedJumpPathUsesFileParent(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: ".campaign/intents/inbox/example.md",
		ItemKind:     wkitem.ItemKindFile,
	}

	got := selectedJumpPath(item)
	want := ".campaign/intents/inbox"
	if got != want {
		t.Fatalf("selectedJumpPath() = %q, want %q", got, want)
	}
}

func TestOutputSelectedPathWritesRelativePath(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: "workflow/design/example",
		ItemKind:     wkitem.ItemKindDirectory,
	}
	outPath := filepath.Join(t.TempDir(), "selected-path")

	if err := outputSelectedPath(item, false, outPath); err != nil {
		t.Fatalf("outputSelectedPath() error = %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output path: %v", err)
	}
	if got := string(data); got != item.RelativePath {
		t.Fatalf("path output = %q, want %q", got, item.RelativePath)
	}
}

func TestValidateFlagsRejectsPathOutputConflicts(t *testing.T) {
	tests := []struct {
		name      string
		jsonMode  bool
		printMode bool
	}{
		{name: "json", jsonMode: true},
		{name: "print", printMode: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlags(tt.jsonMode, tt.printMode, "selected-path", nil, nil)
			if err == nil {
				t.Fatal("validateFlags() error = nil, want conflict")
			}
		})
	}
}
