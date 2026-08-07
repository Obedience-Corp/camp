package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// shelveWorkitem moves the example workitem's directory under a dungeon the way
// promote --target completed does, without invoking the whole promote path.
func shelveWorkitem(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join(root, "workflow", "design", "example")
	relDest := filepath.Join("workflow", "design", ".dungeon", "completed", "2026-07-26", "example")
	dest := filepath.Join(root, relDest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dest); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(relDest)
}

func seedExampleLink(t *testing.T, root string) string {
	t.Helper()
	id := "lnk_20260726_a1b2c3"
	body := "version: " + links.LinksSchemaVersion + "\nlinks:\n" +
		"  - id: " + id + "\n" +
		"    workitem_id: design-example-2026-05-24\n" +
		"    workitem_key: design:workflow/design/example\n" +
		"    scope:\n      kind: worktree\n      path: projects/worktrees/demo/wt\n" +
		"    role: primary\n" +
		"    created_at: 2026-07-26T00:00:00Z\n" +
		"    created_by: test\n"
	dir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "links.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "worktrees", "demo", "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	return id
}

func doctorOutput(t *testing.T, fix bool) string {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	_ = runDoctor(context.Background(), cmd, false, fix)
	return stdout.String()
}

// A link attaches an active workitem to a working location, so a shelved
// workitem should not hold one. Removal is right; what was wrong was the
// diagnosis -- "not present on disk" reads as registry corruption when this is
// ordinary housekeeping. This path covers workitems dungeoned before promote
// learned to release its own links.
func TestDoctor_ShelvedWorkitemLinkIsNamedAndRemoved(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedExampleLink(t, root)
	dungeonRel := shelveWorkitem(t, root)

	out := doctorOutput(t, false)
	if !strings.Contains(out, codeWorkitemShelved) {
		t.Fatalf("expected %s finding, got %q", codeWorkitemShelved, out)
	}
	if strings.Contains(out, "is not present on disk") {
		t.Fatalf("the workitem is shelved, not missing; message should say so: %q", out)
	}
	if !strings.Contains(out, dungeonRel) {
		t.Fatalf("finding should name where the workitem went, got %q", out)
	}

	doctorOutput(t, true)

	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.FindByID(id); ok {
		t.Fatal("auto-fix must still remove a link held by a shelved workitem")
	}
}

func TestDungeonedWorkitems(t *testing.T) {
	root := linkTestCampaign(t)

	if got := dungeonedWorkitems(context.Background(), root); len(got) != 0 {
		t.Fatalf("nothing is shelved yet, got %v", got)
	}

	dungeonRel := shelveWorkitem(t, root)

	got := dungeonedWorkitems(context.Background(), root)
	path, ok := got["design-example-2026-05-24"]
	if !ok {
		t.Fatalf("shelved workitem not found, got %v", got)
	}
	if path != dungeonRel {
		t.Fatalf("path = %q, want %q", path, dungeonRel)
	}
}

func TestUnderDungeon(t *testing.T) {
	cases := map[string]bool{
		"workflow/design/.dungeon/completed/2026-07-26/x/.workitem": true,
		"workflow/design/dungeon/archived/x/.workitem":              true, // pre-migration spelling
		"workflow/design/x/.workitem":                               false,
		"workflow/design/dungeonesque/x/.workitem":                  false,
	}
	for path, want := range cases {
		if got := underDungeon(path); got != want {
			t.Fatalf("underDungeon(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestUnlinkShelvedWorkitem(t *testing.T) {
	root := linkTestCampaign(t)
	id := seedExampleLink(t, root)
	ctx := context.Background()

	const (
		oldID  = "design-example-2026-05-24"
		oldKey = "design:workflow/design/example"
	)

	dropped, err := unlinkShelvedWorkitem(ctx, root, oldID, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 1 || dropped[0].ID != id {
		t.Fatalf("dropped = %+v, want the seeded link", dropped)
	}
	// Enough detail to put the link back by hand.
	if dropped[0].ScopeKind != "worktree" || dropped[0].ScopePath != "projects/worktrees/demo/wt" {
		t.Fatalf("released link missing scope detail: %+v", dropped[0])
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 0 {
		t.Fatalf("registry should be empty, got %+v", registry.Links)
	}

	// Unrelated rows survive.
	seedExampleLink(t, root)
	if dropped, err = unlinkShelvedWorkitem(ctx, root, "design-other-2026-01-01", "design:workflow/design/other"); err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 0 {
		t.Fatalf("a non-matching workitem must drop nothing, got %+v", dropped)
	}
	registry, err = links.Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 1 {
		t.Fatalf("unrelated link was removed: %+v", registry.Links)
	}
}

func TestPrintReleasedLinks(t *testing.T) {
	var out strings.Builder
	err := printReleasedLinks(&out, workitemPromoteResult{
		ID: "demo",
		ReleasedLinks: []releasedLink{{
			ID: "lnk_20260726_a1b2c3", ScopeKind: "worktree",
			ScopePath: "projects/worktrees/demo/wt", Role: "primary",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"lnk_20260726_a1b2c3", "projects/worktrees/demo/wt", "no longer active", "undo:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q missing %q", got, want)
		}
	}

	var quiet strings.Builder
	if err := printReleasedLinks(&quiet, workitemPromoteResult{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	if quiet.String() != "" {
		t.Fatalf("nothing released should print nothing, got %q", quiet.String())
	}
}

// runPromote drives the real command so identity capture, commit inputs, and
// report wiring are exercised together rather than only unlinkShelvedWorkitem
// in isolation.
func runPromote(t *testing.T, target string, extra ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())

	opts := runWorkitemPromoteOptions{
		ID: "design-example-2026-05-24", Target: target, NoCommit: true, Force: true,
	}
	for _, e := range extra {
		if e == "--keep" {
			opts.Keep = true
		}
	}
	_, err := runWorkitemPromote(cmd, opts)
	return stdout.String(), err
}

// End-to-end through the promote command: a dungeon target must release the
// links and say so.
func TestPromoteToDungeon_ReleasesLinksEndToEnd(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedExampleLink(t, root)

	out, err := runPromote(t, "completed")
	if err != nil {
		t.Fatalf("promote: %v (%s)", err, out)
	}
	if !strings.Contains(out, "released link "+id) {
		t.Fatalf("promote must report the released link, got %q", out)
	}
	if !strings.Contains(out, "undo:") {
		t.Fatalf("promote must name the undo, got %q", out)
	}

	registry, lerr := links.Load(context.Background(), root)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, ok := registry.FindByID(id); ok {
		t.Fatal("dungeon promote left the link in place")
	}
}

// --target doc shelves the source too, so it has the same lifecycle boundary.
// Before this was fixed the doc path only shelved, leaving links pointing at a
// workitem the selector could no longer see.
func TestPromoteToDoc_ReleasesLinksWhenSourceIsShelved(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedExampleLink(t, root)
	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "README.md"),
		[]byte("# Example\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPromote(t, "doc")
	if err != nil {
		t.Fatalf("promote: %v (%s)", err, out)
	}
	if !strings.Contains(out, "released link "+id) {
		t.Fatalf("doc promote must release links when it shelves the source, got %q", out)
	}

	registry, lerr := links.Load(context.Background(), root)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, ok := registry.FindByID(id); ok {
		t.Fatal("doc promote left a link to a shelved workitem")
	}
}

// --keep leaves the source active and resolvable, so the link must survive.
func TestPromoteToDoc_KeepPreservesLinks(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedExampleLink(t, root)
	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "README.md"),
		[]byte("# Example\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runPromote(t, "doc", "--keep")
	if err != nil {
		t.Fatalf("promote: %v (%s)", err, out)
	}
	if strings.Contains(out, "released link") {
		t.Fatalf("--keep leaves the source resolvable; nothing should be released: %q", out)
	}

	registry, lerr := links.Load(context.Background(), root)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, ok := registry.FindByID(id); !ok {
		t.Fatal("--keep must not release links")
	}
}
