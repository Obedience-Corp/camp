//go:build container_fs

package workitem

import (
	"context"
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
	seedLink(t, root, links.ScopeWorktree, "projects/worktrees/demo/merged-branch")

	out := runDoctorCapturing(t, false)
	if strings.Contains(out, codeScopeNotLocal) {
		t.Fatalf("a gone worktree whose project is present must not read as machine-local: %q", out)
	}
	if !strings.Contains(out, codeWorktreeGone) {
		t.Fatalf("expected %s finding, got %q", codeWorktreeGone, out)
	}
	if !strings.Contains(out, "projects/demo") {
		t.Fatalf("the finding must name the project the workitem still belongs to: %q", out)
	}
	if !strings.Contains(out, "[info]") {
		t.Fatalf("an expected end of a worktree life is information, not a warning: %q", out)
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
