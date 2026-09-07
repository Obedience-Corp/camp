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

// seedLink writes a single-row links.yaml pointing the example workitem at
// scope, without going through the writer's validation.
func seedLink(t *testing.T, root string, kind links.ScopeKind, path string) string {
	t.Helper()
	id := "lnk_20260726_a1b2c3"
	body := "version: " + links.LinksSchemaVersion + "\nlinks:\n" +
		"  - id: " + id + "\n" +
		"    workitem_id: design-example-2026-05-24\n" +
		"    workitem_key: design:workflow/design/example\n" +
		"    scope:\n      kind: " + string(kind) + "\n      path: " + path + "\n" +
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
	return id
}

func runDoctorCapturing(t *testing.T, fix bool) string {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	_ = runDoctor(context.Background(), cmd, false, fix)
	return stdout.String()
}

// links.yaml is tracked in git, so `doctor --fix` removing a row propagates to
// every machine the campaign syncs to. A worktree that is not on this machine
// is absent, not deleted -- removing its link destroys a row that is correct on
// the machine that owns the worktree.
func TestDoctorFix_DoesNotDeleteMachineLocalWorktreeLink(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedLink(t, root, links.ScopeWorktree, "projects/worktrees/fest/on-another-machine")

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, codeScopeNotLocal) {
		t.Fatalf("expected %s finding, got %q", codeScopeNotLocal, out)
	}
	if strings.Contains(out, codeBrokenScope) {
		t.Fatalf("a machine-local scope must not be reported as broken: %q", out)
	}
	// An absent worktree is the normal state of a synced multi-machine
	// campaign, so it must not be an error-severity finding either -- doctor
	// exits non-zero on any error, and a healthy campaign should exit 0.
	if strings.Contains(out, codeSchemaViolation) {
		t.Fatalf("a machine-local scope must not raise a schema violation: %q", out)
	}
	if !strings.Contains(out, "camp workitem unlink --id "+id) {
		t.Fatalf("finding must name the explicit removal command, got %q", out)
	}

	runDoctorCapturing(t, true)

	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.FindByID(id); !ok {
		t.Fatal("doctor --fix deleted a machine-local worktree link; that loss propagates via git")
	}
}

// A tracked target really is gone everywhere, so auto-fix should still clean it.
func TestDoctorFix_StillRemovesAuthoritativelyDeadScope(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	id := seedLink(t, root, links.ScopeCampaignPath, "workflow/design/deleted")

	out := runDoctorCapturing(t, false)
	if !strings.Contains(out, codeBrokenScope) {
		t.Fatalf("expected %s finding, got %q", codeBrokenScope, out)
	}

	runDoctorCapturing(t, true)

	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.FindByID(id); ok {
		t.Fatal("doctor --fix should have removed a link to a deleted tracked path")
	}
}
