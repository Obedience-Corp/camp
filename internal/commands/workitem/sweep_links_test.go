package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
)

// seedLinkRow writes a single-row links.yaml with the given identity fields, so
// a test can seed a row whose workitem_id deliberately does NOT belong to the
// workitem under test.
func seedLinkRow(t *testing.T, root, linkID, workitemID, workitemKey, scopePath string) {
	t.Helper()
	body := "version: " + links.LinksSchemaVersion + "\nlinks:\n" +
		"  - id: " + linkID + "\n" +
		"    workitem_id: " + workitemID + "\n" +
		"    workitem_key: " + workitemKey + "\n" +
		"    scope:\n      kind: worktree\n      path: " + scopePath + "\n" +
		"    role: primary\n" +
		"    created_at: 2026-07-29T00:00:00Z\n" +
		"    created_by: test\n"
	dir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "links.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(scopePath)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// unadoptedSweepCampaign builds a campaign whose design workitem has NO
// .workitem marker, the shape that makes the slug the only display identity
// available.
func unadoptedSweepCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wiDir := filepath.Join(root, "workflow", "design", "example")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiDir, "README.md"), []byte("# Example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// runSweepOne drives the real per-item sweep against root and returns its stdout
// plus the result entry.
func runSweepOne(t *testing.T, root string, item wkitem.WorkItem) (string, workitemSweepResultItem) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	ctx := context.Background()
	cmd.SetContext(ctx)

	var result workitemSweepResult
	entry := sweepOne(ctx, cmd, &config.CampaignConfig{ID: "test-campaign"}, root,
		wkitem.SweepCandidate{Item: item, Reason: wkitem.EvidenceWorkflowRunCompleted, RunID: "run-001"},
		&result)
	return stdout.String(), entry
}

func loadLinkIDs(t *testing.T, root string) []string {
	t.Helper()
	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(registry.Links))
	for _, l := range registry.Links {
		ids = append(ids, l.ID)
	}
	return ids
}

// The destructive half of a sweep is link deletion, and unlinkShelvedWorkitem
// matches on workitem_id OR workitem_key. An unadopted directory has no marker,
// so its display identity falls back to the slug -- passing that slug as the
// link ID would delete the links of whatever unrelated workitem's real
// workitem_id happens to equal it. Here the seeded link belongs to
// design:workflow/design/other and carries workitem_id "example", which is
// exactly the swept directory's slug. It must survive.
func TestSweepOne_UnadoptedSourceDoesNotReleaseUnrelatedLinkMatchingSlug(t *testing.T) {
	root := unadoptedSweepCampaign(t)
	const linkID = "lnk_20260729_unrelated"
	seedLinkRow(t, root, linkID, "example", "design:workflow/design/other", "projects/worktrees/demo/other")

	out, entry := runSweepOne(t, root, wkitem.WorkItem{
		Key:          "design:workflow/design/example",
		WorkflowType: wkitem.WorkflowTypeDesign,
		RelativePath: filepath.Join("workflow", "design", "example"),
	})

	// The slug fallback is still the display identity; only link matching must
	// refuse it.
	if entry.ID != "example" {
		t.Errorf("entry.ID = %q, want the slug fallback %q", entry.ID, "example")
	}
	if strings.Contains(out, "released link") {
		t.Errorf("an unadopted sweep must not release another workitem's link, got:\n%s", out)
	}
	if got := loadLinkIDs(t, root); len(got) != 1 || got[0] != linkID {
		t.Fatalf("unrelated link was deleted by the sweep: registry = %v", got)
	}
}

// Positive control for the case above: when the source IS adopted, the sweep
// releases the links keyed to its real workitem_id and says so, so the test
// above cannot pass by the sweep simply never unlinking anything.
func TestSweepOne_AdoptedSourceReleasesItsOwnLinks(t *testing.T) {
	root := linkTestCampaign(t)
	linkID := seedExampleLink(t, root)

	out, entry := runSweepOne(t, root, wkitem.WorkItem{
		Key:          "design:workflow/design/example",
		StableID:     "design-example-2026-05-24",
		WorkflowType: wkitem.WorkflowTypeDesign,
		RelativePath: filepath.Join("workflow", "design", "example"),
	})

	if entry.ID != "design-example-2026-05-24" {
		t.Errorf("entry.ID = %q, want the marker id", entry.ID)
	}
	if !strings.Contains(out, "released link "+linkID) {
		t.Errorf("sweep must report the released link, got:\n%s", out)
	}
	if got := loadLinkIDs(t, root); len(got) != 0 {
		t.Fatalf("sweep left a link on a shelved workitem: %v", got)
	}
}

// A link keyed only by workitem_key must still be released when the source is
// unadopted: the key is the identity an unadopted workitem legitimately has, and
// dropping the ID fallback must not cost that.
func TestSweepOne_UnadoptedSourceReleasesLinkMatchingItsKey(t *testing.T) {
	root := unadoptedSweepCampaign(t)
	const linkID = "lnk_20260729_bykey"
	seedLinkRow(t, root, linkID, "", "design:workflow/design/example", "projects/worktrees/demo/example")

	out, _ := runSweepOne(t, root, wkitem.WorkItem{
		Key:          "design:workflow/design/example",
		WorkflowType: wkitem.WorkflowTypeDesign,
		RelativePath: filepath.Join("workflow", "design", "example"),
	})

	if !strings.Contains(out, "released link "+linkID) {
		t.Errorf("a key-matched link must still be released, got:\n%s", out)
	}
	if got := loadLinkIDs(t, root); len(got) != 0 {
		t.Fatalf("key-matched link survived the sweep: %v", got)
	}
}

// sweptWorkitemIdentity is where the two identities diverge, so lock the split
// directly: an unadopted source yields the slug for display and an EMPTY link
// id, while an adopted one yields the marker id for both.
func TestSweptWorkitemIdentity_SeparatesDisplayFromLinkID(t *testing.T) {
	ctx := context.Background()
	exampleRel := wkitem.WorkItem{RelativePath: filepath.Join("workflow", "design", "example")}

	resolve := func(root string) *locate.Location {
		t.Helper()
		loc, err := resolveSweepLocation(root, exampleRel)
		if err != nil {
			t.Fatal(err)
		}
		return loc
	}

	unadopted := resolve(unadoptedSweepCampaign(t))
	ident := sweptWorkitemIdentity(ctx, unadopted, wkitem.WorkItem{Key: "design:workflow/design/example"})
	if ident.LedgerID != "example" {
		t.Errorf("LedgerID = %q, want the slug fallback", ident.LedgerID)
	}
	if ident.LinkID != "" {
		t.Errorf("LinkID = %q, want empty for an unadopted source", ident.LinkID)
	}

	adopted := resolve(linkTestCampaign(t))
	ident = sweptWorkitemIdentity(ctx, adopted, wkitem.WorkItem{Key: "design:workflow/design/example"})
	if ident.LedgerID != "design-example-2026-05-24" || ident.LinkID != "design-example-2026-05-24" {
		t.Errorf("adopted source identity = %+v, want the marker id in both", ident)
	}

	// A file-shaped source has no .workitem sibling for LoadMetadata to read, so
	// the link id comes from the id discovery already resolved from its own
	// frontmatter. Still a real marker id, never a slug.
	ident = sweptWorkitemIdentity(ctx, unadopted, wkitem.WorkItem{StableID: "design-file-2026-07-29"})
	if ident.LinkID != "design-file-2026-07-29" {
		t.Errorf("LinkID = %q, want the discovered StableID when no marker is readable", ident.LinkID)
	}

	// A readable marker is authoritative over a StableID resolved earlier.
	ident = sweptWorkitemIdentity(ctx, adopted, wkitem.WorkItem{StableID: "design-stale-2026-07-29"})
	if ident.LinkID != "design-example-2026-05-24" {
		t.Errorf("LinkID = %q, want the marker id to win over StableID", ident.LinkID)
	}
}
