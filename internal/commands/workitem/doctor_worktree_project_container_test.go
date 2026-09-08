//go:build container_fs

package workitem

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// These stage a campaign on disk, so they run inside the pooled container
// rather than on a developer's machine (decision D007). They keep package
// scope, which is what lets them assert on doctor's unexported finding codes.

// A worktree is deleted as a matter of course once its branch merges. The
// workitem's real subject is the project that worktree checked out, so once the
// directory is gone the registry still knows where the work belongs and doctor
// has nothing to warn about.
func TestDoctor_GoneWorktreeWithLivingProjectIsInformational(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// linkTestCampaign creates projects/demo; the worktree under it never
	// existed here, which is what a merged-and-removed one looks like
	// afterwards.
	id := seedLink(t, root, links.ScopeWorktree, "projects/worktrees/demo/merged-branch")

	out := runDoctorCapturing(t, false)
	if strings.Contains(out, codeScopeNotLocal) {
		t.Fatalf("a gone worktree whose project is present must not read as machine-local: %q", out)
	}
	if !strings.Contains(out, codeWorktreeGone) {
		t.Fatalf("expected %s finding, got %q", codeWorktreeGone, out)
	}
	if !strings.Contains(out, "[info]") {
		t.Fatalf("an expected end of a worktree life is information, not a warning: %q", out)
	}
	// The finding can never be cleared, so it is one summary row rather than a
	// permanent line per link.
	if !strings.Contains(out, "1 worktree link(s) point at worktrees that are gone") {
		t.Fatalf("expected the collapsed summary, got %q", out)
	}
	// The summary is registry-scoped. Other findings may still name this link,
	// so the assertion is about the gone-worktree row itself.
	goneRow := findingRow(t, out, codeWorktreeGone)
	if strings.Contains(goneRow, "link:"+id) {
		t.Fatalf("the gone-worktree finding must not name each link: %q", goneRow)
	}
	if !strings.Contains(goneRow, "registry") {
		t.Fatalf("the gone-worktree finding must be registry-scoped: %q", goneRow)
	}
}

// findingRow returns the doctor output line carrying the given finding code.
func findingRow(t *testing.T, out, code string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, code) {
			return line
		}
	}
	t.Fatalf("no %s row in:\n%s", code, out)
	return ""
}

// Many gone worktrees are one line, not one line each. A health command whose
// output grows forever with rows nobody can act on stops being read.
func TestDoctor_GoneWorktreesCollapseIntoOneLine(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	seedLinks(t, root, map[string]string{
		"lnk_20260907_00000a": "projects/worktrees/demo/one",
		"lnk_20260907_00000b": "projects/worktrees/demo/two",
		"lnk_20260907_00000c": "projects/worktrees/demo/three",
	})

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, "3 worktree link(s) point at worktrees that are gone") {
		t.Fatalf("expected one summary row naming the count, got %q", out)
	}
	if got := strings.Count(out, codeWorktreeGone); got != 1 {
		t.Fatalf("%s appears %d times, want exactly 1: %q", codeWorktreeGone, got, out)
	}
}

// The project relationship is only durable once it is written down: derivation
// covers reading, and --fix is what records it.
func TestDoctorFix_BackfillsProjectOnWorktreeLinks(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedLink(t, root, links.ScopeWorktree, "projects/worktrees/demo/merged-branch")

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, codeWorktreeProjectUnknown) {
		t.Fatalf("expected %s finding, got %q", codeWorktreeProjectUnknown, out)
	}

	runDoctorCapturing(t, true)

	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	link, ok := registry.FindByID(id)
	if !ok {
		t.Fatal("doctor --fix must not remove the link it is repairing")
	}
	if link.Scope.Project != "projects/demo" {
		t.Fatalf("Scope.Project = %q, want projects/demo", link.Scope.Project)
	}

	after := runDoctorCapturing(t, false)
	if strings.Contains(after, codeWorktreeProjectUnknown) {
		t.Fatalf("the finding must clear once the project is recorded: %q", after)
	}
}

// A worktree path camp cannot read has no project to record and no project to
// fall back on, so the honest answer is still the machine-local warning.
func TestDoctor_UnreadableWorktreePathKeepsTheMachineLocalWarning(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	seedLink(t, root, links.ScopeWorktree, "projects/worktrees/stray")

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, codeScopeNotLocal) {
		t.Fatalf("expected %s finding, got %q", codeScopeNotLocal, out)
	}
	if strings.Contains(out, codeWorktreeProjectUnknown) {
		t.Fatalf("camp must not offer to record a project it cannot derive: %q", out)
	}
}

// A worktree whose project is genuinely absent (an uncloned submodule, another
// machine's checkout) must keep reading as machine-local: camp cannot tell the
// user the work still belongs somewhere it cannot see.
func TestDoctor_GoneWorktreeWithAbsentProjectStaysMachineLocal(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	seedLink(t, root, links.ScopeWorktree, "projects/worktrees/never-cloned/branch")

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, codeScopeNotLocal) {
		t.Fatalf("expected %s finding, got %q", codeScopeNotLocal, out)
	}
	if strings.Contains(out, codeWorktreeGone) {
		t.Fatalf("camp cannot claim the workitem stays linked to a project that is not here: %q", out)
	}
}

// seedLinks writes a links.yaml with one worktree row per id, all pointing at
// the example workitem, without going through the writer's validation.
func seedLinks(t *testing.T, root string, rows map[string]string) {
	t.Helper()
	body := "version: " + links.LinksSchemaVersion + "\nlinks:\n"
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		body += "  - id: " + id + "\n" +
			"    workitem_id: design-example-2026-05-24\n" +
			"    workitem_key: design:workflow/design/example\n" +
			"    scope:\n      kind: worktree\n      path: " + rows[id] + "\n" +
			"    role: related\n" +
			"    created_at: 2026-09-07T00:00:00Z\n" +
			"    created_by: test\n"
	}
	dir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "links.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
