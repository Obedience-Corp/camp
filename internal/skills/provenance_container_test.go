//go:build container_fs

package skills

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeSkill(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, ".campaign", "skills", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", msg)
}

func writtenOnceAnswers(t *testing.T, root string, rels ...string) map[string]bool {
	t.Helper()
	prov := NewGitProvenance(root)
	got := make(map[string]bool, len(rels))
	for _, rel := range rels {
		once, err := prov.WrittenOnce(t.Context(), ".campaign/skills/"+rel)
		if err != nil {
			t.Fatalf("WrittenOnce(%s) error = %v", rel, err)
		}
		got[rel] = once
	}
	return got
}

func TestGitProvenanceCancelledScanIsNotKept(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "once/SKILL.md", "v1\n")
	gitIn(t, root, "init", "-q")
	commitAll(t, root, "camp init")
	prov := NewGitProvenance(root)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prov.WrittenOnce(cancelled, ".campaign/skills/once/SKILL.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("WrittenOnce(cancelled) error = %v, want context.Canceled", err)
	}
	once, err := prov.WrittenOnce(t.Context(), ".campaign/skills/once/SKILL.md")
	if err != nil || !once {
		t.Fatalf("WrittenOnce() after a cancelled scan = %v, %v; want true from a fresh scan", once, err)
	}
}

func TestGitProvenanceWrittenOnce(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"once/SKILL.md", "twice/SKILL.md", "dirty/SKILL.md", "staged/SKILL.md", "merged/SKILL.md"} {
		writeSkill(t, root, rel, "v1\n")
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	commitAll(t, root, "camp init")
	writeSkill(t, root, "twice/SKILL.md", "v1 plus an edit\n")
	commitAll(t, root, "edit a skill")

	gitIn(t, root, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(root, "side.md"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "side work")
	gitIn(t, root, "checkout", "-q", "main")
	gitIn(t, root, "merge", "-q", "--no-ff", "--no-commit", "side")
	writeSkill(t, root, "merged/SKILL.md", "edited inside the merge\n")
	commitAll(t, root, "merge side with an edit")

	writeSkill(t, root, "dirty/SKILL.md", "uncommitted edit\n")
	writeSkill(t, root, "staged/SKILL.md", "staged edit\n")
	gitIn(t, root, "add", ".campaign/skills/staged/SKILL.md")
	writeSkill(t, root, "staged/SKILL.md", "v1\n")
	writeSkill(t, root, "untracked/SKILL.md", "v1\n")

	got := writtenOnceAnswers(t, root, "once/SKILL.md", "twice/SKILL.md", "merged/SKILL.md",
		"dirty/SKILL.md", "staged/SKILL.md", "untracked/SKILL.md", "absent/SKILL.md")
	want := map[string]bool{"once/SKILL.md": true}
	for rel, ok := range got {
		if ok != want[rel] {
			t.Errorf("WrittenOnce(%s) = %v, want %v", rel, ok, want[rel])
		}
	}
}

func TestGitProvenanceCampBelowGitRoot(t *testing.T) {
	top := t.TempDir()
	root := filepath.Join(top, "camps", "mine")
	writeSkill(t, root, "once/SKILL.md", "v1\n")
	gitIn(t, top, "init", "-q")
	commitAll(t, top, "camp init")

	if got := writtenOnceAnswers(t, root, "once/SKILL.md"); !got["once/SKILL.md"] {
		t.Fatal("WrittenOnce() = false for a camp below the git root; paths must be camp-root relative")
	}
}

func TestGitProvenanceWithoutUsableHistoryOffersNoEvidence(t *testing.T) {
	plain := t.TempDir()
	writeSkill(t, plain, "once/SKILL.md", "v1\n")

	unborn := t.TempDir()
	writeSkill(t, unborn, "once/SKILL.md", "v1\n")
	gitIn(t, unborn, "init", "-q")
	gitIn(t, unborn, "add", "-A")

	origin := t.TempDir()
	writeSkill(t, origin, "once/SKILL.md", "v1\n")
	gitIn(t, origin, "init", "-q")
	commitAll(t, origin, "camp init")
	writeSkill(t, origin, "once/SKILL.md", "v1 plus an edit\n")
	commitAll(t, origin, "edit a skill")
	shallow := filepath.Join(t.TempDir(), "clone")
	gitIn(t, origin, "clone", "-q", "--depth", "1", "file://"+origin, shallow)

	for name, root := range map[string]string{"not a repository": plain, "no commits": unborn, "shallow clone": shallow} {
		if writtenOnceAnswers(t, root, "once/SKILL.md")["once/SKILL.md"] {
			t.Errorf("%s: WrittenOnce() = true, want no evidence", name)
		}
	}
}

func TestUpdateReplacesGitEvidenceFileWithUndo(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha/SKILL.md", "alpha v1\n")
	writeSkill(t, root, "beta/SKILL.md", "beta v1\n")
	gitIn(t, root, "init", "-q")
	commitAll(t, root, "camp init")
	writeSkill(t, root, "beta/SKILL.md", "beta v1 plus my rule\n")
	commitAll(t, root, "my rule")
	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
	}

	res, err := Update(t.Context(), UpdateOptions{Root: root, Shipped: shipped, CampVersion: "v2"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	actions := map[string]FileAction{}
	for _, f := range res.Files {
		actions[f.Bundle] = f.Action
		if f.Bundle == "alpha" && (!f.Written || !f.GitEvidence) {
			t.Errorf("alpha = %+v, want written on git evidence", f)
		}
	}
	if actions["alpha"] != ActionUpdate || actions["beta"] != ActionPreserve {
		t.Fatalf("actions = %v, want alpha updated and beta preserved", actions)
	}
	wantUndo := []Undo{{Bundle: "alpha", Command: "git checkout HEAD -- .campaign/skills/alpha/SKILL.md"}}
	if !slices.Equal(res.Undo, wantUndo) {
		t.Fatalf("Undo = %+v, want %+v", res.Undo, wantUndo)
	}
	inv, err := ClassifyCamp(t.Context(), root, shipped)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ManifestWritten || inv.Manifest == nil {
		t.Fatal("Update() must record the manifest")
	}
	if hash, ok := inv.Manifest.recorded("alpha", "SKILL.md"); !ok || hash != shipped[0].Hash() {
		t.Errorf("alpha record = %q (present %v), want the shipped hash", hash, ok)
	}
	if _, ok := inv.Manifest.recorded("beta", "SKILL.md"); ok {
		t.Error("the preserved beta file must stay unrecorded")
	}

	cmd := exec.Command("sh", "-c", res.Undo[0].Command)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("undo %q: %v\n%s", res.Undo[0].Command, err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, ".campaign", "skills", "alpha", "SKILL.md")); string(got) != "alpha v1\n" {
		t.Fatalf("after undo alpha = %q, want the content camp replaced", got)
	}
	if inv, err = ClassifyCamp(t.Context(), root, shipped); err != nil {
		t.Fatal(err)
	}
	if inv.Files[0].State != ShippedModified {
		t.Errorf("after undo alpha = %q, want modified as the printed undo note says", inv.Files[0].State)
	}
}
