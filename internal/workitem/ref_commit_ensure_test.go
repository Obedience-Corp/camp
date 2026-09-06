package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMinimalCampaignForRef(t *testing.T, root string) {
	t.Helper()
	dirs := []string{
		".campaign/intents/inbox",
		".campaign/intents/active",
		".campaign/intents/ready",
		"workflow/design",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := `version: campaign/v1
id: testcampaign
name: test
type: product
`
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRefForCommit_MintsIntentRef(t *testing.T) {
	root := t.TempDir()
	writeMinimalCampaignForRef(t, root)
	rel := ".campaign/intents/active/mint-me.md"
	writeFile(t, filepath.Join(root, rel), strings.Join([]string{
		"---",
		"id: mint-me-20260101",
		"title: Mint Me",
		"status: active",
		"created_at: 2026-01-01",
		"---",
		"",
		"Body.",
	}, "\n"))

	wi := WorkItem{
		WorkflowType: WorkflowTypeIntent,
		ItemKind:     ItemKindFile,
		SourceID:     "mint-me-20260101",
		RelativePath: rel,
		SourceMetadata: map[string]any{
			"intent_type": "feature",
		},
	}

	ref, err := EnsureRefForCommit(context.Background(), root, &wi, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("EnsureRefForCommit() error = %v", err)
	}
	if ref == "" {
		t.Fatal("expected minted ref, got empty string")
	}
	if !strings.HasPrefix(ref, RefPrefix) {
		t.Fatalf("ref = %q, want WI- prefix", ref)
	}
	want := Derive("mint-me-20260101")
	if ref != want {
		t.Fatalf("ref = %q, want derived %q", ref, want)
	}

	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ref: "+ref) {
		t.Fatalf("intent file missing backfilled ref:\n%s", raw)
	}
}

func TestEnsureRefForCommit_ReturnsExistingIntentRefWithoutRewrite(t *testing.T) {
	root := t.TempDir()
	writeMinimalCampaignForRef(t, root)
	rel := ".campaign/intents/active/has-ref.md"
	original := strings.Join([]string{
		"---",
		"id: has-ref-20260101",
		"ref: WI-existing",
		"title: Has Ref",
		"status: active",
		"created_at: 2026-01-01",
		"---",
		"",
		"Body.",
	}, "\n")
	path := filepath.Join(root, rel)
	writeFile(t, path, original)

	wi := WorkItem{
		WorkflowType: WorkflowTypeIntent,
		ItemKind:     ItemKindFile,
		SourceID:     "has-ref-20260101",
		RelativePath: rel,
		SourceMetadata: map[string]any{
			"ref": "WI-existing",
		},
	}

	ref, err := EnsureRefForCommit(context.Background(), root, &wi, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("EnsureRefForCommit() error = %v", err)
	}
	if ref != "WI-existing" {
		t.Fatalf("ref = %q, want WI-existing", ref)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("intent file rewritten when ref already set:\n%s", got)
	}
}

func TestEnsureRefForCommit_BackfillFailureReturnsEmptyWithWarning(t *testing.T) {
	root := t.TempDir()
	writeMinimalCampaignForRef(t, root)
	rel := ".campaign/intents/inbox/broken.md"
	writeFile(t, filepath.Join(root, rel), "no frontmatter\n")

	wi := WorkItem{
		WorkflowType: WorkflowTypeIntent,
		ItemKind:     ItemKindFile,
		SourceID:     "broken-20260101",
		RelativePath: rel,
		SourceMetadata: map[string]any{},
	}

	var errw bytes.Buffer
	ref, err := EnsureRefForCommit(context.Background(), root, &wi, &errw)
	if err != nil {
		t.Fatalf("EnsureRefForCommit() error = %v, want nil so commit proceeds", err)
	}
	if ref != "" {
		t.Fatalf("ref = %q, want empty on backfill failure", ref)
	}
	if !strings.Contains(errw.String(), "warning:") {
		t.Fatalf("expected warning on stderr, got %q", errw.String())
	}
}
