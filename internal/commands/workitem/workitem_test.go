package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestValidateFlagsAcceptsStageNoneForNoStageTypes(t *testing.T) {
	if err := validateFlags(true, false, false, "", []string{"design"}, []string{"none"}, nil, nil, "attention_stage"); err != nil {
		t.Fatalf("validateFlags(design, none) error = %v", err)
	}
	if err := validateFlags(true, false, false, "", []string{"explore"}, []string{"none"}, nil, nil, "attention_stage"); err != nil {
		t.Fatalf("validateFlags(explore, none) error = %v", err)
	}
}

func TestValidateFlagsRejectsStageForWrongType(t *testing.T) {
	if err := validateFlags(true, false, false, "", []string{"intent"}, []string{"planning"}, nil, nil, "attention_stage"); err == nil {
		t.Fatal("validateFlags(intent, planning) error = nil, want invalid stage")
	}
	if err := validateFlags(true, false, false, "", []string{"design"}, []string{"inbox"}, nil, nil, "attention_stage"); err == nil {
		t.Fatal("validateFlags(design, inbox) error = nil, want invalid stage")
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
	if _, err := captureStdout(func() error {
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

func TestWorkitemJSONUsesResolvedRootAndRelativePaths(t *testing.T) {
	root := linkTestCampaign(t)
	link := filepath.Join(t.TempDir(), "campaign-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink campaign root: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", root, err)
	}

	restore := chdir(t, link)
	defer restore()

	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--json", "--type", "design", "--limit", "1"})
	cmd.SetErr(io.Discard)
	stdout, err := captureStdout(func() error {
		return cmd.ExecuteContext(context.Background())
	})
	if err != nil {
		t.Fatalf("workitem --json: %v", err)
	}

	var payload struct {
		CampaignRoot string `json:"campaign_root"`
		Items        []struct {
			RelativePath string `json:"relative_path"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("workitem JSON invalid: %v\nraw: %s", err, stdout)
	}
	if payload.CampaignRoot != resolvedRoot {
		t.Fatalf("campaign_root = %q, want %q", payload.CampaignRoot, resolvedRoot)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(payload.Items))
	}
	path := payload.Items[0].RelativePath
	if filepath.IsAbs(path) {
		t.Fatalf("workitem relative_path is absolute: %q", path)
	}
	if _, err := os.Stat(filepath.Join(payload.CampaignRoot, path)); err != nil {
		t.Fatalf("joined workitem path missing for %q: %v", path, err)
	}
}

func TestOutputListGroupsByGroup(t *testing.T) {
	var out bytes.Buffer
	items := []wkitem.WorkItem{
		{
			Key:            "design:workflow/design/example",
			WorkflowType:   wkitem.WorkflowTypeDesign,
			Title:          "Example Workitem",
			RelativePath:   "workflow/design/example",
			SortTimestamp:  time.Now(),
			AttentionStage: "next",
			Group:          "camp-workflow",
		},
		{
			Key:            "design:workflow/design/other",
			WorkflowType:   wkitem.WorkflowTypeDesign,
			Title:          "Other Workitem",
			RelativePath:   "workflow/design/other",
			SortTimestamp:  time.Now(),
			AttentionStage: "active",
		},
	}

	if err := outputList(&out, items, "group"); err != nil {
		t.Fatalf("outputList() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"CAMP-WORKFLOW", "UNGROUPED", "next", "active", "Example Workitem", "workflow/design/example"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func captureStdout(fn func() error) (string, error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = r.Close()
	}()

	runErr := fn()
	_ = w.Close()
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		return "", readErr
	}
	return string(out), runErr
}

func TestValidateFlagsAcceptsBuiltinAndCustomTypes(t *testing.T) {
	cases := []string{
		"intent", "design", "explore", "festival",
		"feature", "bug", "incident", "rfc-001", "deep_dive",
		"PascalCase", "camelCase", "v1.2",
	}
	for _, tname := range cases {
		t.Run(tname, func(t *testing.T) {
			if err := validateFlags(false, false, false, "", []string{tname}, nil, nil, nil, "attention_stage"); err != nil {
				t.Fatalf("validateFlags(--type=%q) = %v, want nil", tname, err)
			}
		})
	}
}

func TestValidateFlagsRejectsInvalidTypeSlugs(t *testing.T) {
	cases := []string{"with space", "has/slash", "-leading", ".hidden", ""}
	for _, tname := range cases {
		t.Run(tname, func(t *testing.T) {
			if err := validateFlags(false, false, false, "", []string{tname}, nil, nil, nil, "attention_stage"); err == nil {
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
			err := validateFlags(tt.jsonMode, false, tt.printMode, "selected-path", nil, nil, nil, nil, "attention_stage")
			if err == nil {
				t.Fatal("validateFlags() error = nil, want conflict")
			}
		})
	}
}

func TestValidateFlagsRejectsListConflicts(t *testing.T) {
	tests := []struct {
		name       string
		jsonMode   bool
		printMode  bool
		pathOutput string
	}{
		{name: "json", jsonMode: true},
		{name: "print", printMode: true},
		{name: "path-output", pathOutput: "selected-path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlags(tt.jsonMode, true, tt.printMode, tt.pathOutput, nil, nil, nil, nil, "attention_stage")
			if err == nil {
				t.Fatal("validateFlags() error = nil, want conflict")
			}
		})
	}
}
