package workitem

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
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

func TestSelectedDefaultActionOpensFileItems(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: ".campaign/intents/inbox/example.md",
		ItemKind:     wkitem.ItemKindFile,
	}

	if got := selectedDefaultAction(item); got != selectedActionOpenEditor {
		t.Fatalf("selectedDefaultAction() = %q, want %q", got, selectedActionOpenEditor)
	}
}

func TestSelectedDefaultActionJumpsDirectoryItems(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: "workflow/design/example",
		ItemKind:     wkitem.ItemKindDirectory,
	}

	if got := selectedDefaultAction(item); got != selectedActionJumpDirectory {
		t.Fatalf("selectedDefaultAction() = %q, want %q", got, selectedActionJumpDirectory)
	}
}

func TestSelectedOpenPathUsesPrimaryDoc(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: ".campaign/intents/inbox/example.md",
		PrimaryDoc:   ".campaign/intents/inbox/example.md",
		ItemKind:     wkitem.ItemKindFile,
	}

	got := selectedOpenPath(item, "/campaign")
	want := filepath.Join("/campaign", ".campaign/intents/inbox/example.md")
	if got != want {
		t.Fatalf("selectedOpenPath() = %q, want %q", got, want)
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

func TestWorkitemListNoPruneOnRead(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	store := priority.NewStore()
	priority.Set(store, "design:workflow/design/transiently-missing", priority.High)
	storePath := priority.StorePath(root)
	if err := priority.Save(storePath, store); err != nil {
		t.Fatalf("save priority store: %v", err)
	}
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read priority store before list: %v", err)
	}

	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--json"})
	cmd.SetErr(io.Discard)
	if err := captureStdout(func() error {
		return cmd.ExecuteContext(context.Background())
	}); err != nil {
		t.Fatalf("workitem --json: %v", err)
	}

	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read priority store after list: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("workitem --json mutated priority store during read")
	}
}

func captureStdout(fn func() error) error {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = r.Close()
	}()

	runErr := fn()
	_ = w.Close()
	_, _ = io.Copy(io.Discard, r)
	return runErr
}

func TestValidateFlagsAcceptsBuiltinAndCustomTypes(t *testing.T) {
	cases := []string{
		"intent", "design", "explore", "festival",
		"feature", "bug", "incident", "rfc-001", "deep_dive",
		"PascalCase", "camelCase", "v1.2",
	}
	for _, tname := range cases {
		t.Run(tname, func(t *testing.T) {
			if err := validateFlags(false, false, "", []string{tname}, nil); err != nil {
				t.Fatalf("validateFlags(--type=%q) = %v, want nil", tname, err)
			}
		})
	}
}

func TestValidateFlagsRejectsInvalidTypeSlugs(t *testing.T) {
	cases := []string{"with space", "has/slash", "-leading", ".hidden", ""}
	for _, tname := range cases {
		t.Run(tname, func(t *testing.T) {
			if err := validateFlags(false, false, "", []string{tname}, nil); err == nil {
				t.Fatalf("validateFlags(--type=%q) = nil, want validation error", tname)
			}
		})
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
