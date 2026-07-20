package skills

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeSkillBundle(t *testing.T, skillsDir, slug string) {
	t.Helper()
	dir := filepath.Join(skillsDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+slug+"\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestListWorktreeRoots(t *testing.T) {
	tmp := t.TempDir()
	// Layout: worktrees/camp/feat-a, worktrees/fest/pr-1 — only dirs with .git count.
	for _, p := range []string{
		filepath.Join(tmp, "camp", "feat-a"),
		filepath.Join(tmp, "camp", "feat-b"),
		filepath.Join(tmp, "fest", "pr-1"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, ".git"), []byte("gitdir: /tmp/fake\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tmp, "camp", "not-a-worktree", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "camp", ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "camp", "notadir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListWorktreeRoots(tmp)
	if err != nil {
		t.Fatalf("ListWorktreeRoots: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d roots %v, want 3", len(got), got)
	}
	if got[0] != filepath.Join(tmp, "camp", "feat-a") {
		t.Errorf("first = %q", got[0])
	}
}

func TestListWorktreeRootsSkipsNonGitChildren(t *testing.T) {
	tmp := t.TempDir()
	// Mis-nested: project dir is itself a git checkout; children are src/bin.
	proj := filepath.Join(tmp, "fest-gif")
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".git"), []byte("gitdir: /tmp/fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ListWorktreeRoots(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != proj {
		t.Fatalf("got %v, want only the git root %s", got, proj)
	}
}

func TestListWorktreeRootsMissing(t *testing.T) {
	got, err := ListWorktreeRoots(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestProjectIntoWorktree(t *testing.T) {
	campaignRoot := t.TempDir()
	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	writeSkillBundle(t, skillsDir, "camp-navigation")
	writeSkillBundle(t, skillsDir, "fest-execution")

	wt := filepath.Join(campaignRoot, "projects", "worktrees", "camp", "feature-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	slugs, err := DiscoverSkillSlugs(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := ProjectIntoWorktree(wt, campaignRoot, skillsDir, slugs, false, false, io.Discard)
	if err != nil {
		t.Fatalf("ProjectIntoWorktree: %v", err)
	}
	// 2 skill bundles under .agents + 1 for creating .grok/skills alias.
	if proj.Agents.Created != 3 {
		t.Errorf("agents created = %d, want 3 (2 skills + grok alias)", proj.Agents.Created)
	}
	if proj.Claude.Created != 2 {
		t.Errorf("claude created = %d, want 2", proj.Claude.Created)
	}

	for _, rel := range []string{
		".agents/skills/camp-navigation",
		".agents/skills/fest-execution",
		".claude/skills/camp-navigation",
	} {
		p := filepath.Join(wt, rel)
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink", rel)
		}
		target, err := os.Readlink(p)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(target) {
			// relative targets should still resolve into .campaign/skills
			abs := resolveSymlinkTargetAbs(p, target)
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("%s -> %s does not resolve: %v", rel, target, err)
			}
		}
	}

	// Grok alias
	grokLink := filepath.Join(wt, GrokSkillsRel)
	info, err := os.Lstat(grokLink)
	if err != nil {
		t.Fatalf("grok skills alias missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("grok skills alias is not a symlink")
	}
	raw, err := os.Readlink(grokLink)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "../.agents/skills" {
		t.Errorf("grok alias target = %q, want ../.agents/skills", raw)
	}

	// Idempotent second run
	proj2, err := ProjectIntoWorktree(wt, campaignRoot, skillsDir, slugs, false, false, io.Discard)
	if err != nil {
		t.Fatalf("second ProjectIntoWorktree: %v", err)
	}
	// 2 skill links already linked + grok alias already linked.
	if proj2.Agents.AlreadyLinked != 3 || proj2.Agents.Created != 0 {
		t.Errorf("second run agents: created=%d linked=%d, want created=0 linked=3",
			proj2.Agents.Created, proj2.Agents.AlreadyLinked)
	}
}

func TestLinkAllWorktrees(t *testing.T) {
	campaignRoot := t.TempDir()
	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	writeSkillBundle(t, skillsDir, "alpha")

	wtRoot := filepath.Join(campaignRoot, "projects", "worktrees")
	for _, p := range []string{
		filepath.Join(wtRoot, "camp", "a"),
		filepath.Join(wtRoot, "fest", "b"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, ".git"), []byte("gitdir: /tmp/fake\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	results, err := LinkAllWorktrees(campaignRoot, wtRoot, skillsDir, false, false, io.Discard)
	if err != nil {
		t.Fatalf("LinkAllWorktrees: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("worktree %s: %v", r.Path, r.Err)
		}
		// 1 skill bundle + 1 grok alias create.
		if r.Agents.Created != 2 {
			t.Errorf("%s agents created = %d, want 2", r.RelPath, r.Agents.Created)
		}
		if _, err := os.Lstat(filepath.Join(r.Path, ".agents/skills", "alpha")); err != nil {
			t.Errorf("missing projection in %s: %v", r.Path, err)
		}
	}
}

func TestInspectWorktreeProjection(t *testing.T) {
	campaignRoot := t.TempDir()
	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	writeSkillBundle(t, skillsDir, "alpha")
	slugs, _ := DiscoverSkillSlugs(skillsDir)

	wt := filepath.Join(campaignRoot, "projects", "worktrees", "camp", "x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := InspectWorktreeProjection(wt, skillsDir, slugs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Linked != 0 || st.TotalSkills != 1 {
		t.Errorf("before project: linked=%d total=%d", st.Linked, st.TotalSkills)
	}

	if _, err := ProjectIntoWorktree(wt, campaignRoot, skillsDir, slugs, false, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	st, err = InspectWorktreeProjection(wt, skillsDir, slugs)
	if err != nil {
		t.Fatal(err)
	}
	if st.Linked != 1 {
		t.Errorf("after project: linked=%d, want 1", st.Linked)
	}
}

func TestEnsureGrokSkillsAliasIdempotent(t *testing.T) {
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGrokSkillsAlias(wt, false); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGrokSkillsAlias(wt, false); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestEnsureWorktreeGrokSkillsForeignSymlinkRequiresForce(t *testing.T) {
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, ".grok"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(wt, GrokSkillsRel)
	if err := os.Symlink("/somewhere/else", linkPath); err != nil {
		t.Fatal(err)
	}

	summary, err := ensureWorktreeGrokSkills(wt, "", nil, false, false, io.Discard)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if summary.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1", summary.Conflicts)
	}
	// Link must still point at foreign target.
	raw, _ := os.Readlink(linkPath)
	if raw != "/somewhere/else" {
		t.Errorf("foreign symlink was modified without force: %q", raw)
	}

	summary, err = ensureWorktreeGrokSkills(wt, "", nil, false, true, io.Discard)
	if err != nil {
		t.Fatalf("force replace: %v", err)
	}
	if summary.Replaced != 1 {
		t.Errorf("replaced = %d, want 1", summary.Replaced)
	}
	raw, err = os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if raw != grokAliasRelTarget {
		t.Errorf("after force target = %q, want %q", raw, grokAliasRelTarget)
	}
}

func TestEnsureWorktreeGrokSkillsProjectsIntoRealDirectory(t *testing.T) {
	campaignRoot := t.TempDir()
	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	writeSkillBundle(t, skillsDir, "camp-navigation")
	slugs, _ := DiscoverSkillSlugs(skillsDir)

	wt := filepath.Join(campaignRoot, "projects", "worktrees", "camp", "dir-grok")
	grokSkills := filepath.Join(wt, GrokSkillsRel)
	if err := os.MkdirAll(grokSkills, 0o755); err != nil {
		t.Fatal(err)
	}

	summary, err := ensureWorktreeGrokSkills(wt, skillsDir, slugs, false, false, io.Discard)
	if err != nil {
		t.Fatalf("project into dir: %v", err)
	}
	if summary.Created != 1 {
		t.Errorf("created = %d, want 1", summary.Created)
	}
	if _, err := os.Lstat(filepath.Join(grokSkills, "camp-navigation")); err != nil {
		t.Errorf("managed link missing in .grok/skills dir: %v", err)
	}
	// Directory must remain a directory, not be replaced by a symlink.
	info, err := os.Lstat(grokSkills)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Error(".grok/skills should remain a real directory")
	}
}

func TestProjectIntoWorktreeBestEffort(t *testing.T) {
	campaignRoot := t.TempDir()
	wt := filepath.Join(campaignRoot, "projects", "worktrees", "camp", "x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	// No skills dir: no-op, not projected.
	projected, err := ProjectIntoWorktreeBestEffort(campaignRoot, wt)
	if err != nil {
		t.Fatalf("missing skills dir: %v", err)
	}
	if projected {
		t.Error("projected=true with no skills dir")
	}

	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Empty skills dir: no-op.
	projected, err = ProjectIntoWorktreeBestEffort(campaignRoot, wt)
	if err != nil {
		t.Fatalf("empty skills dir: %v", err)
	}
	if projected {
		t.Error("projected=true with empty skills dir")
	}

	writeSkillBundle(t, skillsDir, "camp-navigation")
	projected, err = ProjectIntoWorktreeBestEffort(campaignRoot, wt)
	if err != nil {
		t.Fatalf("with skills: %v", err)
	}
	if !projected {
		t.Error("projected=false after successful projection")
	}
	if _, err := os.Lstat(filepath.Join(wt, ".agents", "skills", "camp-navigation")); err != nil {
		t.Errorf("link missing: %v", err)
	}

	// Second call still projected (already-linked counts).
	projected, err = ProjectIntoWorktreeBestEffort(campaignRoot, wt)
	if err != nil {
		t.Fatalf("idempotent: %v", err)
	}
	if !projected {
		t.Error("projected=false when already linked")
	}
}
