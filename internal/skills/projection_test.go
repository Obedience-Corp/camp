package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsManagedSkillEntryLink_ManagedLink(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	slugDir := filepath.Join(skillsDir, "code-review")
	if err := os.MkdirAll(slugDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	linkPath := filepath.Join(destDir, "code-review")
	rel, err := filepath.Rel(destDir, slugDir)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if err := os.Symlink(rel, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	managed, err := IsManagedSkillEntryLink(linkPath, slugDir, skillsDir)
	if err != nil {
		t.Fatalf("IsManagedSkillEntryLink: %v", err)
	}
	if !managed {
		t.Error("expected managed=true for link pointing into .campaign/skills")
	}
}

func TestIsManagedSkillEntryLink_ForeignLink(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	outsideDir := resolvePath(t, t.TempDir())
	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	linkPath := filepath.Join(destDir, "foreign")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	expectedTarget := filepath.Join(skillsDir, "foreign")
	managed, err := IsManagedSkillEntryLink(linkPath, expectedTarget, skillsDir)
	if err != nil {
		t.Fatalf("IsManagedSkillEntryLink: %v", err)
	}
	if managed {
		t.Error("expected managed=false for foreign symlink")
	}
}

func TestIsManagedSkillEntryLink_NotALink(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	regularFile := filepath.Join(tmpDir, "regular")
	if err := os.WriteFile(regularFile, []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	managed, err := IsManagedSkillEntryLink(regularFile, skillsDir, skillsDir)
	if err != nil {
		t.Fatalf("IsManagedSkillEntryLink: %v", err)
	}
	if managed {
		t.Error("expected managed=false for regular file")
	}
}

func TestIsManagedSkillEntryLink_Missing(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	managed, err := IsManagedSkillEntryLink(
		filepath.Join(tmpDir, "nonexistent"),
		filepath.Join(tmpDir, "target"),
		filepath.Join(tmpDir, "skills"),
	)
	if err != nil {
		t.Fatalf("IsManagedSkillEntryLink: %v", err)
	}
	if managed {
		t.Error("expected managed=false for missing path")
	}
}

func TestIsManagedSkillEntryLink_BrokenManagedLink(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a symlink that points into .campaign/skills but target doesn't exist
	missingSlug := filepath.Join(skillsDir, "deleted-skill")
	linkPath := filepath.Join(destDir, "deleted-skill")
	rel, err := filepath.Rel(destDir, missingSlug)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if err := os.Symlink(rel, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	managed, err := IsManagedSkillEntryLink(linkPath, missingSlug, skillsDir)
	if err != nil {
		t.Fatalf("IsManagedSkillEntryLink: %v", err)
	}
	if !managed {
		t.Error("expected managed=true for broken link whose target is inside .campaign/skills")
	}
}

func TestProjectSkillEntries_BasicProjection(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	for _, slug := range []string{"alpha", "beta"} {
		dir := filepath.Join(skillsDir, slug)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+slug), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	summary, err := ProjectSkillEntries(destDir, skillsDir, []string{"alpha", "beta"}, false, false)
	if err != nil {
		t.Fatalf("ProjectSkillEntries: %v", err)
	}
	if summary.Created != 2 {
		t.Errorf("expected 2 created, got %d", summary.Created)
	}
	if summary.Conflicts != 0 {
		t.Errorf("expected 0 conflicts, got %d", summary.Conflicts)
	}

	// Verify links exist
	for _, slug := range []string{"alpha", "beta"} {
		state, err := CheckLinkState(filepath.Join(destDir, slug), filepath.Join(skillsDir, slug))
		if err != nil {
			t.Fatalf("CheckLinkState: %v", err)
		}
		if state != StateValid {
			t.Errorf("slug %q: expected StateValid, got %q", slug, state)
		}
	}
}

func TestProjectSkillEntries_Idempotent(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	slugDir := filepath.Join(skillsDir, "alpha")
	if err := os.MkdirAll(slugDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(slugDir, "SKILL.md"), []byte("name: alpha"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// First projection
	_, err := ProjectSkillEntries(destDir, skillsDir, []string{"alpha"}, false, false)
	if err != nil {
		t.Fatalf("first ProjectSkillEntries: %v", err)
	}

	// Second projection should be idempotent
	summary, err := ProjectSkillEntries(destDir, skillsDir, []string{"alpha"}, false, false)
	if err != nil {
		t.Fatalf("second ProjectSkillEntries: %v", err)
	}
	if summary.Created != 0 {
		t.Errorf("expected 0 created on second run, got %d", summary.Created)
	}
	if summary.AlreadyLinked != 1 {
		t.Errorf("expected 1 already linked, got %d", summary.AlreadyLinked)
	}
}

func TestInspectSkillProjection_MixedStates(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	for _, slug := range []string{"linked", "conflict", "mismatch"} {
		dir := filepath.Join(skillsDir, slug)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// "linked" — valid symlink to correct target
	rel, _ := filepath.Rel(destDir, filepath.Join(skillsDir, "linked"))
	if err := os.Symlink(rel, filepath.Join(destDir, "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// "conflict" — regular directory (not a link)
	if err := os.MkdirAll(filepath.Join(destDir, "conflict"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// "mismatch" — symlink to a different valid target
	otherDir := filepath.Join(tmpDir, "other")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(otherDir, filepath.Join(destDir, "mismatch")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	state, err := InspectSkillProjection(destDir, skillsDir, []string{"linked", "conflict", "mismatch"})
	if err != nil {
		t.Fatalf("InspectSkillProjection: %v", err)
	}
	if state.Linked != 1 {
		t.Errorf("expected 1 linked, got %d", state.Linked)
	}
	if state.Conflicts < 1 {
		t.Errorf("expected at least 1 conflict, got %d", state.Conflicts)
	}
}

func TestRemoveProjectedSkillEntries_OnlyRemovesManaged(t *testing.T) {
	t.Parallel()
	tmpDir := resolvePath(t, t.TempDir())

	skillsDir := filepath.Join(tmpDir, ".campaign", "skills")
	slugDir := filepath.Join(skillsDir, "alpha")
	if err := os.MkdirAll(slugDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	destDir := filepath.Join(tmpDir, ".claude", "skills")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a managed link
	rel, _ := filepath.Rel(destDir, slugDir)
	if err := os.Symlink(rel, filepath.Join(destDir, "alpha")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	removed, err := RemoveProjectedSkillEntries(destDir, skillsDir, []string{"alpha"}, false)
	if err != nil {
		t.Fatalf("RemoveProjectedSkillEntries: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	// Verify link is gone
	state, err := CheckLinkState(filepath.Join(destDir, "alpha"), slugDir)
	if err != nil {
		t.Fatalf("CheckLinkState: %v", err)
	}
	if state != StateMissing {
		t.Errorf("expected StateMissing after removal, got %q", state)
	}
}

func TestToolPaths_ReturnsCopy(t *testing.T) {
	t.Parallel()
	paths := ToolPaths()
	paths["evil"] = "should/not/mutate"

	// Second call should not contain the mutation
	paths2 := ToolPaths()
	if _, ok := paths2["evil"]; ok {
		t.Error("ToolPaths() returned a mutable reference to the internal map")
	}
}

func TestToolNames_Sorted(t *testing.T) {
	t.Parallel()
	names := ToolNames()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 tool names, got %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("ToolNames not sorted: %v", names)
			break
		}
	}
}
