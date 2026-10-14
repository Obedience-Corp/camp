//go:build container_fs

package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dirlink"
)

func TestSkillProjectionDirectoryLinkReadback(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source", "alpha")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".agents", "skills", "alpha")
	if err := CreateSkillProjectionLink(link, source, false); err != nil {
		t.Fatal(err)
	}
	st, err := dirlink.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && st.Kind != dirlink.KindJunction {
		t.Fatalf("local skill link kind = %v, want junction", st.Kind)
	}
	state, err := CheckLinkState(link, source)
	if err != nil || state != StateValid {
		t.Fatalf("CheckLinkState = %q, %v; want valid", state, err)
	}
	managed, err := IsManagedSkillEntryLink(link, source, filepath.Dir(source))
	if err != nil || !managed {
		t.Fatalf("IsManagedSkillEntryLink = %v, %v; want true", managed, err)
	}
	if got, err := os.ReadFile(filepath.Join(link, "SKILL.md")); err != nil || string(got) != "alpha" {
		t.Fatalf("skill readback = %q, %v", got, err)
	}
	if err := dirlink.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatalf("removing skill link touched source: %v", err)
	}
}

func TestGrokSkillsAliasDirectoryLink(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "keep.txt"), []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := createGrokSkillsAlias(root, false); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, GrokSkillsRel)
	st, err := dirlink.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && st.Kind != dirlink.KindJunction {
		t.Fatalf("local Grok alias kind = %v, want junction", st.Kind)
	}
	if got, err := os.ReadFile(filepath.Join(link, "keep.txt")); err != nil || string(got) != "stay" {
		t.Fatalf("Grok alias readback = %q, %v", got, err)
	}
	if err := dirlink.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "keep.txt")); err != nil {
		t.Fatalf("removing Grok alias touched skills: %v", err)
	}
}
