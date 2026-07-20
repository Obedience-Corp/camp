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
	if proj.Agents.Created != 2 {
		t.Errorf("agents created = %d, want 2", proj.Agents.Created)
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
	if proj2.Agents.AlreadyLinked != 2 || proj2.Agents.Created != 0 {
		t.Errorf("second run agents: created=%d linked=%d, want created=0 linked=2",
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
		if r.Agents.Created != 1 {
			t.Errorf("%s agents created = %d", r.RelPath, r.Agents.Created)
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
