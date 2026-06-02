package skills

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeBundle(t *testing.T, skillsDir, slug string) {
	t.Helper()
	dir := filepath.Join(skillsDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+slug+"\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func resultByTool(results []LinkResult, tool string) (LinkResult, bool) {
	for _, r := range results {
		if r.Tool == tool {
			return r, true
		}
	}
	return LinkResult{}, false
}

func TestLinkDefaultTools_FreshProjectsAllTools(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	writeBundle(t, skillsDir, "code-review")
	writeBundle(t, skillsDir, "deep-research")

	results, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard)
	if err != nil {
		t.Fatalf("LinkDefaultTools: %v", err)
	}
	if len(results) != len(ToolNames()) {
		t.Fatalf("expected %d results, got %d", len(ToolNames()), len(results))
	}

	for _, tool := range ToolNames() {
		res, ok := resultByTool(results, tool)
		if !ok {
			t.Fatalf("missing result for tool %q", tool)
		}
		if res.Err != nil {
			t.Errorf("tool %q error: %v", tool, res.Err)
		}
		if res.Summary.Created != 2 {
			t.Errorf("tool %q expected 2 created, got %d", tool, res.Summary.Created)
		}
		relPath, _ := ResolveToolPath(tool)
		link := filepath.Join(root, relPath, "code-review")
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("expected projected link %s: %v", link, err)
		}
	}
}

func TestLinkDefaultTools_Idempotent(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	writeBundle(t, skillsDir, "code-review")

	if _, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard); err != nil {
		t.Fatalf("first link: %v", err)
	}
	results, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard)
	if err != nil {
		t.Fatalf("second link: %v", err)
	}
	for _, res := range results {
		if res.Summary.Created != 0 {
			t.Errorf("tool %q expected 0 created on rerun, got %d", res.Tool, res.Summary.Created)
		}
		if res.Summary.AlreadyLinked != 1 {
			t.Errorf("tool %q expected 1 already-linked on rerun, got %d", res.Tool, res.Summary.AlreadyLinked)
		}
	}
}

func TestLinkDefaultTools_HealsBrokenManagedLink(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	writeBundle(t, skillsDir, "code-review")

	if _, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard); err != nil {
		t.Fatalf("initial link: %v", err)
	}

	relPath, _ := ResolveToolPath("claude")
	link := filepath.Join(root, relPath, "code-review")
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove link: %v", err)
	}

	results, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard)
	if err != nil {
		t.Fatalf("repair link: %v", err)
	}
	res, _ := resultByTool(results, "claude")
	if res.Summary.Created != 1 {
		t.Errorf("expected 1 recreated link, got created=%d", res.Summary.Created)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("expected restored link %s: %v", link, err)
	}
}

func TestLinkDefaultTools_RealFileIsConflictNotOverwritten(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	writeBundle(t, skillsDir, "code-review")

	relPath, _ := ResolveToolPath("claude")
	destDir := filepath.Join(root, relPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	realFile := filepath.Join(destDir, "code-review")
	if err := os.WriteFile(realFile, []byte("user data"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	results, err := LinkDefaultTools(root, skillsDir, false, true, io.Discard)
	if err != nil {
		t.Fatalf("LinkDefaultTools: %v", err)
	}
	res, _ := resultByTool(results, "claude")
	if res.Summary.Conflicts != 1 {
		t.Errorf("expected 1 conflict, got %d", res.Summary.Conflicts)
	}
	content, err := os.ReadFile(realFile)
	if err != nil {
		t.Fatalf("read real file: %v", err)
	}
	if string(content) != "user data" {
		t.Errorf("real file overwritten: got %q", string(content))
	}
}

func TestLinkDefaultTools_DryRunTouchesNothing(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	writeBundle(t, skillsDir, "code-review")

	results, err := LinkDefaultTools(root, skillsDir, true, false, io.Discard)
	if err != nil {
		t.Fatalf("LinkDefaultTools dry-run: %v", err)
	}
	for _, res := range results {
		if res.Summary.Created != 1 {
			t.Errorf("tool %q expected 1 would-create, got %d", res.Tool, res.Summary.Created)
		}
		relPath, _ := ResolveToolPath(res.Tool)
		if _, err := os.Lstat(filepath.Join(root, relPath, "code-review")); !os.IsNotExist(err) {
			t.Errorf("tool %q dry-run created a link", res.Tool)
		}
	}
}

func TestLinkDefaultTools_EmptySkillsDir(t *testing.T) {
	t.Parallel()
	root := resolvePath(t, t.TempDir())
	skillsDir := filepath.Join(root, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, err := LinkDefaultTools(root, skillsDir, false, false, io.Discard)
	if err != nil {
		t.Fatalf("LinkDefaultTools: %v", err)
	}
	for _, res := range results {
		if res.Summary.Created != 0 {
			t.Errorf("tool %q expected nothing created for empty skills dir, got %d", res.Tool, res.Summary.Created)
		}
	}
}
