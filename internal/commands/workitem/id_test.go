package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func runIDCmd(t *testing.T, ctx context.Context, args []string, opts idOptions) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runID(ctx, cmd, args, opts)
	return stdout.String(), err
}

// writeWorkitemDir seeds a directory workitem with a stable .workitem id.
func writeWorkitemDir(t *testing.T, root, relDir, wtype, id string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := "version: v1alpha5\nkind: workitem\nid: " + id + "\ntype: " + wtype + "\ntitle: " + id + "\n"
	if err := os.WriteFile(filepath.Join(dir, wkitem.MetadataFilename), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFestival seeds a festival directory with a fest.yaml metadata id.
func writeFestival(t *testing.T, root, stage, name, id string) {
	t.Helper()
	dir := filepath.Join(root, "festivals", stage, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: v1alpha1\nmetadata:\n  id: " + id + "\n  name: " + id + "\n  festival_type: standard\n"
	if err := os.WriteFile(filepath.Join(dir, "fest.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestID_NoContextIsError(t *testing.T) {
	root := linkTestCampaign(t)
	other := filepath.Join(root, "docs")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := chdir(t, other)
	defer restore()

	if _, err := runIDCmd(t, context.Background(), nil, idOptions{}); err == nil {
		t.Fatal("expected an error when no workitem is in context and no selector is given")
	}
}

func TestID_NonexistentSelectorIsError(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if _, err := runIDCmd(t, context.Background(), []string{"does-not-exist"}, idOptions{}); err == nil {
		t.Fatal("expected an error for a selector that matches nothing")
	}
}

func TestID_NonexistentPathIsError(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// A path-looking argument that is not on disk is treated as a selector and
	// must fail loudly rather than silently succeed.
	if _, err := runIDCmd(t, context.Background(), []string{"workflow/design/missing"}, idOptions{}); err == nil {
		t.Fatal("expected an error for a nonexistent campaign-relative path")
	}
}

func TestID_AmbiguousSelectorIsError(t *testing.T) {
	root := linkTestCampaign(t)
	writeWorkitemDir(t, root, "workflow/design/dup", "design", "design-dup-a")
	writeWorkitemDir(t, root, "workflow/explore/dup", "explore", "explore-dup-b")
	restore := chdir(t, root)
	defer restore()

	// "dup" matches the directory-slug tier against two workitems.
	if _, err := runIDCmd(t, context.Background(), []string{"dup"}, idOptions{}); err == nil {
		t.Fatal("expected an ambiguity error when a slug matches multiple workitems")
	}
}

func TestID_StableIDIsDefault(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{testWorkitemID}, idOptions{})
	if err != nil {
		t.Fatalf("runID: %v", err)
	}
	if strings.TrimSpace(out) != testWorkitemID {
		t.Fatalf("id = %q, want %q", strings.TrimSpace(out), testWorkitemID)
	}
}

func TestID_KeyFlagPrintsKey(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{testWorkitemID}, idOptions{Key: true})
	if err != nil {
		t.Fatalf("runID --key: %v", err)
	}
	if strings.TrimSpace(out) != testWorkitemKey {
		t.Fatalf("key = %q, want %q", strings.TrimSpace(out), testWorkitemKey)
	}
}

func TestID_CwdInsideWorkitemResolvesViaAncestor(t *testing.T) {
	root := linkTestCampaign(t)
	cwd := filepath.Join(root, "workflow", "design", "example")
	restore := chdir(t, cwd)
	defer restore()

	out, err := runIDCmd(t, context.Background(), nil, idOptions{})
	if err != nil {
		t.Fatalf("runID: %v", err)
	}
	if strings.TrimSpace(out) != testWorkitemID {
		t.Fatalf("id = %q, want %q", strings.TrimSpace(out), testWorkitemID)
	}
}

func TestID_ExplicitSelectorBeatsCwd(t *testing.T) {
	root := linkTestCampaign(t)
	writeWorkitemDir(t, root, "workflow/design/other", "design", "design-other-2026-05-24")
	// cwd is inside the "example" workitem, but an explicit selector for
	// "other" must win (explicit tier precedes the cwd-ancestor tier).
	cwd := filepath.Join(root, "workflow", "design", "example")
	restore := chdir(t, cwd)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{"design-other-2026-05-24"}, idOptions{})
	if err != nil {
		t.Fatalf("runID: %v", err)
	}
	if strings.TrimSpace(out) != "design-other-2026-05-24" {
		t.Fatalf("id = %q, want design-other-2026-05-24", strings.TrimSpace(out))
	}
}

func TestID_CampaignRelativePathResolves(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{"workflow/design/example"}, idOptions{})
	if err != nil {
		t.Fatalf("runID path: %v", err)
	}
	if strings.TrimSpace(out) != testWorkitemID {
		t.Fatalf("id = %q, want %q", strings.TrimSpace(out), testWorkitemID)
	}
}

func TestID_AbsoluteFilesystemPathResolves(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	abs := filepath.Join(root, "workflow", "design", "example")
	out, err := runIDCmd(t, context.Background(), []string{abs}, idOptions{})
	if err != nil {
		t.Fatalf("runID abs path: %v", err)
	}
	if strings.TrimSpace(out) != testWorkitemID {
		t.Fatalf("id = %q, want %q", strings.TrimSpace(out), testWorkitemID)
	}
}

func TestID_FestivalDurableIDIsFestivalMetadataID(t *testing.T) {
	root := linkTestCampaign(t)
	writeFestival(t, root, "active", "social-SC0001", "SC0001")
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{"SC0001"}, idOptions{})
	if err != nil {
		t.Fatalf("runID festival: %v", err)
	}
	if strings.TrimSpace(out) != "SC0001" {
		t.Fatalf("id = %q, want SC0001", strings.TrimSpace(out))
	}
}

func TestID_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{testWorkitemID}, idOptions{JSON: true})
	if err != nil {
		t.Fatalf("runID --json: %v", err)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		ID            string `json:"id"`
		IDKind        string `json:"id_kind"`
		Key           string `json:"key"`
		StableID      string `json:"stable_id"`
		Source        string `json:"source"`
		RelativePath  string `json:"relative_path"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out)
	}
	if payload.SchemaVersion != WorkitemIDJSONVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, WorkitemIDJSONVersion)
	}
	if payload.ID != testWorkitemID || payload.IDKind != string(idKindStable) {
		t.Fatalf("id/id_kind = %q/%q", payload.ID, payload.IDKind)
	}
	if payload.Key != testWorkitemKey || payload.StableID != testWorkitemID {
		t.Fatalf("key/stable_id = %q/%q", payload.Key, payload.StableID)
	}
	if payload.Source != "explicit" || payload.RelativePath != "workflow/design/example" {
		t.Fatalf("source/relative_path = %q/%q", payload.Source, payload.RelativePath)
	}
}

func TestID_JSONKeyFallbackForItemWithoutStableID(t *testing.T) {
	root := linkTestCampaign(t)
	plain := filepath.Join(root, "workflow", "design", "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "README.md"), []byte("# Plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := chdir(t, root)
	defer restore()

	out, err := runIDCmd(t, context.Background(), []string{"workflow/design/plain"}, idOptions{JSON: true})
	if err != nil {
		t.Fatalf("runID --json: %v", err)
	}
	var payload struct {
		ID       string `json:"id"`
		IDKind   string `json:"id_kind"`
		Key      string `json:"key"`
		StableID string `json:"stable_id"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out)
	}
	if payload.IDKind != string(idKindKey) {
		t.Fatalf("id_kind = %q, want key", payload.IDKind)
	}
	if payload.ID != payload.Key || payload.Key != "design:workflow/design/plain" {
		t.Fatalf("id/key = %q/%q, want both design:workflow/design/plain", payload.ID, payload.Key)
	}
	if payload.StableID != "" {
		t.Fatalf("stable_id = %q, want empty for a key-fallback item", payload.StableID)
	}
}

func TestDurableID_MatchesLinkTarget(t *testing.T) {
	cases := []struct {
		name string
		wi   wkitem.WorkItem
		want idKind
	}{
		{"stable", wkitem.WorkItem{StableID: "design-x-2026-05-24", Key: "design:workflow/design/x"}, idKindStable},
		{"festival", wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeFestival, SourceID: "SC0001", Key: "festival:festivals/active/x"}, idKindFestival},
		{"key_only", wkitem.WorkItem{Key: "design:workflow/design/plain"}, idKindKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, kind := durableID(&tc.wi)
			if want := wkitem.LinkWorkitemID(&tc.wi); id != want {
				t.Fatalf("durableID value = %q, want LinkWorkitemID %q (must not drift)", id, want)
			}
			if kind != tc.want {
				t.Fatalf("id_kind = %q, want %q", kind, tc.want)
			}
		})
	}
}

func TestWorkitemSelectorCandidates(t *testing.T) {
	items := []wkitem.WorkItem{
		{StableID: "design-a", Key: "design:workflow/design/a", Title: "Alpha"},
		{StableID: "design-b", Key: "design:workflow/design/b", Title: "Bravo"},
		{WorkflowType: wkitem.WorkflowTypeFestival, SourceID: "SC0001", Key: "festival:festivals/active/x", Title: "Camp"},
		{Key: "design:workflow/design/plain", Title: "Plain"}, // key fallback
	}

	tests := []struct {
		name       string
		toComplete string
		want       []string
	}{
		{
			name:       "all with titles",
			toComplete: "",
			want: []string{
				"SC0001\tCamp",
				"design-a\tAlpha",
				"design-b\tBravo",
				"design:workflow/design/plain\tPlain",
			},
		},
		{
			name:       "prefix filters",
			toComplete: "design-",
			want:       []string{"design-a\tAlpha", "design-b\tBravo"},
		},
		{
			name:       "festival prefix",
			toComplete: "SC",
			want:       []string{"SC0001\tCamp"},
		},
		{
			name:       "no match",
			toComplete: "zzz",
			want:       []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := workitemSelectorCandidates(items, tc.toComplete)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("candidate[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestWorkitemSelectorCandidates_Dedup(t *testing.T) {
	items := []wkitem.WorkItem{
		{StableID: "design-a", Key: "design:workflow/design/a", Title: "Alpha"},
		{StableID: "design-a", Key: "design:workflow/design/a", Title: "Alpha copy"},
	}
	got := workitemSelectorCandidates(items, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated candidate, got %v", got)
	}
}

func TestCompleteWorkitemSelector_Wired(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	got, directive := completeWorkitemSelector(cmd, nil, "design")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	found := false
	for _, c := range got {
		if strings.HasPrefix(c, testWorkitemID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected completion to include %q, got %v", testWorkitemID, got)
	}
}

func TestCompleteWorkitemSelector_NoCompletionAfterFirstArg(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	got, directive := completeWorkitemSelector(cmd, []string{"already"}, "")
	if got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected no candidates after the first positional arg, got %v (%v)", got, directive)
	}
}
