//go:build container_fs

package skills

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// linkedPluginCamp is a camp with festival-app added by camp-app, linked into
// both camp-root tool folders and into one project worktree, beside alpha.
func linkedPluginCamp(t *testing.T) (root, worktrees string) {
	t.Helper()
	root = t.TempDir()
	if _, err := Update(t.Context(), UpdateOptions{
		Root: root, Shipped: campShipped(), Plugins: []PluginBundle{offered("0.4.0", newSkill)},
		CampVersion: "v2",
	}); err != nil {
		t.Fatal(err)
	}
	updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{})
	skillsDir := filepath.Join(root, ".campaign", "skills")
	slugs := []string{"alpha", "festival-app"}
	for _, rel := range []string{".claude/skills", ".agents/skills"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := ProjectSkillEntries(filepath.Join(root, rel), skillsDir, slugs, false, false); err != nil {
			t.Fatal(err)
		}
	}
	worktrees = filepath.Join(root, "projects", "worktrees")
	wt := filepath.Join(worktrees, "app", "feature")
	if err := os.MkdirAll(filepath.Join(wt, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(wt, ".agents", "skills")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectSkillEntries(dest, skillsDir, slugs, false, false); err != nil {
		t.Fatal(err)
	}
	return root, worktrees
}

func TestDeclineRemovesBundleAndLinksAndRecordsIt(t *testing.T) {
	root, worktrees := linkedPluginCamp(t)
	res, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees})
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	wantLinks := []string{".agents/skills/festival-app", ".claude/skills/festival-app", "projects/worktrees/app/feature/.agents/skills/festival-app"}
	if !res.Removed || !slices.Equal(res.LinksRemoved, wantLinks) || res.Restore != nil {
		t.Fatalf("Decline() = %+v, want removed with links %v", res, wantLinks)
	}
	for _, p := range append(wantLinks, ".campaign/skills/festival-app") {
		if fileExists(filepath.Join(root, p)) {
			t.Errorf("%s still exists", p)
		}
	}
	if !fileExists(filepath.Join(root, ".claude", "skills", "alpha")) {
		t.Error("decline removed another bundle's link")
	}
	m := readCampManifest(t, root)
	if m.Providers["festival-app"] != (ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0", Declined: true}) || m.Bundles["festival-app"] != nil {
		t.Fatalf("manifest = %+v / %v, want a declined record without files", m.Providers, m.Bundles["festival-app"])
	}
	again, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees})
	if err != nil || again.Removed || again.ManifestWritten {
		t.Fatalf("second Decline() = %+v, %v; want nothing to do", again, err)
	}
}

func TestDeclineKeepsForeignEntries(t *testing.T) {
	root, _ := linkedPluginCamp(t)
	claude := filepath.Join(root, ".claude", "skills", "festival-app")
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(claude) {
		t.Fatal("decline removed a real directory that was not camp's link")
	}
}

func TestDeclineRefusesAnEditUnlessForcedAndBacksUpUntrackedFiles(t *testing.T) {
	root, _ := linkedPluginCamp(t)
	bundle := filepath.Join(root, ".campaign", "skills", "festival-app")
	if err := os.WriteFile(filepath.Join(bundle, "notes.md"), []byte("my notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin})
	if !errors.Is(err, ErrDeclineEdited) || !fileExists(filepath.Join(bundle, "notes.md")) {
		t.Fatalf("Decline() error = %v, want ErrDeclineEdited with nothing removed", err)
	}
	res, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, Force: true})
	if err != nil || res.Restore == nil || res.Restore.BackupDir == "" {
		t.Fatalf("forced Decline() = %+v, %v; want a backup", res, err)
	}
	restore := exec.Command("sh", "-c", res.Restore.Command)
	restore.Dir = root
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restore %q: %v\n%s", res.Restore.Command, err, out)
	}
	if data, err := os.ReadFile(filepath.Join(bundle, "notes.md")); err != nil || string(data) != "my notes\n" {
		t.Fatalf("restored notes = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(bundle, "SKILL.md")); err != nil || string(data) != newSkill {
		t.Fatalf("restored SKILL.md = %q, %v", data, err)
	}
}

func TestDeclineRefusesSymlinksEvenForced(t *testing.T) {
	for name, stage := range map[string]func(t *testing.T, skillsDir, outside string){
		"symlinked bundle": func(t *testing.T, skillsDir, outside string) {
			if err := os.Symlink(outside, filepath.Join(skillsDir, "festival-app")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink inside": func(t *testing.T, skillsDir, outside string) {
			if err := os.MkdirAll(filepath.Join(skillsDir, "festival-app"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(skillsDir, "festival-app", "refs")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "keep.md"), []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			skillsDir := filepath.Join(root, ".campaign", "skills")
			if err := os.MkdirAll(skillsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			stage(t, skillsDir, outside)
			_, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, Force: true})
			if !errors.Is(err, ErrDeclineShape) || !fileExists(filepath.Join(outside, "keep.md")) || !fileExists(filepath.Join(skillsDir, "festival-app")) {
				t.Fatalf("Decline() error = %v; want ErrDeclineShape and nothing removed", err)
			}
		})
	}
}

func TestDeclineWithoutABundleOnlyRecordsIt(t *testing.T) {
	root := t.TempDir()
	res, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, CampVersion: "v2"})
	if err != nil || res.Removed || !res.ManifestWritten {
		t.Fatalf("Decline() = %+v, %v; want only a record", res, err)
	}
	if m := readCampManifest(t, root); !m.Providers["festival-app"].Declined {
		t.Fatalf("providers = %+v, want festival-app declined", m.Providers)
	}
	if _, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "../escape", Plugin: CampAppPlugin}); err == nil {
		t.Fatal("Decline() accepted a slug that is not a bundle name")
	}
}

func TestLinkBundlesFolderCases(t *testing.T) {
	root, _ := linkedPluginCamp(t)
	skillsDir := filepath.Join(root, ".campaign", "skills")
	if err := os.Remove(filepath.Join(root, ".claude", "skills", "festival-app")); err != nil {
		t.Fatal(err)
	}
	links, err := LinkBundles(root, skillsDir, []string{"festival-app"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []BundleLink{
		{Tool: "agents", Path: ".agents/skills/festival-app", Result: LinkAlreadyLinked},
		{Tool: "claude", Path: ".claude/skills/festival-app", Result: LinkLinked},
	}
	if !slices.Equal(links, want) || !fileExists(filepath.Join(root, ".claude", "skills", "festival-app", "SKILL.md")) {
		t.Fatalf("camp-linked folders: %+v, want %+v", links, want)
	}

	marker := "managed_by: obey\nkind: skills_projection\n"
	if err := os.WriteFile(filepath.Join(root, ".agents", "skills", ObeyManagedMarkerFile), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".agents", "skills", "festival-app")); err != nil {
		t.Fatal(err)
	}
	links, err = LinkBundles(root, skillsDir, []string{"festival-app"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want = []BundleLink{
		{Tool: "agents", Path: ".agents/skills", Result: LinkLeftToObey},
		{Tool: "claude", Path: ".claude/skills", Result: LinkNoFolder},
	}
	if !slices.Equal(links, want) || fileExists(filepath.Join(root, ".claude")) || fileExists(filepath.Join(root, ".agents", "skills", "festival-app")) {
		t.Fatalf("marked and missing folders: %+v, want %+v and nothing created", links, want)
	}
}

func TestLinkBundlesLeavesUnmanagedFoldersAndConflicts(t *testing.T) {
	root, _ := linkedPluginCamp(t)
	skillsDir := filepath.Join(root, ".campaign", "skills")
	if err := os.RemoveAll(filepath.Join(root, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "my-own"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeEntry := filepath.Join(root, ".claude", "skills", "festival-app")
	if err := os.Remove(claudeEntry); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeEntry, 0o755); err != nil {
		t.Fatal(err)
	}
	links, err := LinkBundles(root, skillsDir, []string{"festival-app"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []BundleLink{
		{Tool: "agents", Path: ".agents/skills", Result: LinkLeftUnmanaged},
		{Tool: "claude", Path: ".claude/skills/festival-app", Result: LinkConflict},
	}
	if !slices.Equal(links, want) || fileExists(filepath.Join(root, ".agents", "skills", "festival-app")) {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
}

// A decline cut short at each step must leave a camp an automatic request
// leaves alone, a plain update does not damage, and a second decline finishes.
func TestDeclineResumesFromEveryIntermediateState(t *testing.T) {
	for name, stop := range map[string]func(t *testing.T, root string){
		"recorded, nothing removed": func(*testing.T, string) {},
		"links removed": func(t *testing.T, root string) {
			removeAll(t, filepath.Join(root, ".claude", "skills", "festival-app"), filepath.Join(root, ".agents", "skills", "festival-app"))
		},
		"folder removed, hashes kept": func(t *testing.T, root string) {
			removeAll(t, filepath.Join(root, ".claude", "skills", "festival-app"), filepath.Join(root, ".agents", "skills", "festival-app"),
				filepath.Join(root, ".campaign", "skills", "festival-app"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, worktrees := linkedPluginCamp(t)
			skillsDir := filepath.Join(root, ".campaign", "skills")
			m := readCampManifest(t, root)
			m.Providers["festival-app"] = ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0", Declined: true}
			if err := writeManifest(skillsDir, m); err != nil {
				t.Fatal(err)
			}
			stop(t, root)
			hadFolder := fileExists(filepath.Join(skillsDir, "festival-app"))

			res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Auto: true})
			if res.Files[0].State != ShippedDeclined || res.Files[0].Written || fileExists(filepath.Join(skillsDir, "festival-app")) != hadFolder {
				t.Fatalf("automatic request touched a half-declined bundle: %+v", res.Files[0])
			}
			if _, err := Update(t.Context(), UpdateOptions{Root: root, Shipped: campShipped(), CampVersion: "v2"}); err != nil {
				t.Fatal(err)
			}
			if after := readCampManifest(t, root); !after.Providers["festival-app"].Declined || after.Bundles["festival-app"]["SKILL.md"] == "" {
				t.Fatalf("a plain update lost the half-declined record: %+v %v", after.Providers, after.Bundles)
			}

			if _, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees}); err != nil {
				t.Fatalf("second Decline() error = %v", err)
			}
			for _, p := range []string{".campaign/skills/festival-app", ".claude/skills/festival-app", ".agents/skills/festival-app",
				"projects/worktrees/app/feature/.agents/skills/festival-app"} {
				if fileExists(filepath.Join(root, p)) {
					t.Errorf("%s still exists", p)
				}
			}
			final := readCampManifest(t, root)
			if !final.Providers["festival-app"].Declined || final.Bundles["festival-app"] != nil {
				t.Fatalf("manifest = %+v / %v, want declined without hashes", final.Providers, final.Bundles["festival-app"])
			}
		})
	}
}

func removeAll(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeclineRefusesOddShapesPrecisely(t *testing.T) {
	for name, tc := range map[string]struct {
		stage func(t *testing.T, skillsDir string)
		want  string
	}{
		"bundle is a file": {func(t *testing.T, skillsDir string) {
			if err := os.WriteFile(filepath.Join(skillsDir, "festival-app"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, ".campaign/skills/festival-app is a file, not a folder"},
		"SKILL.md is a folder": {func(t *testing.T, skillsDir string) {
			if err := os.MkdirAll(filepath.Join(skillsDir, "festival-app", "SKILL.md"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, ".campaign/skills/festival-app/SKILL.md is a folder, not a file"},
		"a symlink inside": {func(t *testing.T, skillsDir string) {
			if err := os.MkdirAll(filepath.Join(skillsDir, "festival-app", "notes"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc", filepath.Join(skillsDir, "festival-app", "notes", "etc")); err != nil {
				t.Fatal(err)
			}
		}, ".campaign/skills/festival-app holds a symlink at notes/etc"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			skillsDir := filepath.Join(root, ".campaign", "skills")
			if err := os.MkdirAll(skillsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			tc.stage(t, skillsDir)
			_, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, Force: true})
			if !errors.Is(err, ErrDeclineShape) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decline() error = %v, want %q", err, tc.want)
			}
			if !fileExists(filepath.Join(skillsDir, "festival-app")) {
				t.Fatal("decline removed what it refused")
			}
		})
	}
}

// Cut a decline short after each step, the way a crash or a full disk would.
// The camp must already count the bundle as declined, and a second decline
// must finish the job.
func TestDeclineCutShortAfterEachStepIsDeclinedAndResumes(t *testing.T) {
	for _, step := range []string{"recorded", "links", "folder"} {
		t.Run(step, func(t *testing.T) {
			root, worktrees := linkedPluginCamp(t)
			skillsDir := filepath.Join(root, ".campaign", "skills")
			declineStep = func(at string) error {
				if at == step {
					return errors.New("cut short at " + at)
				}
				return nil
			}
			_, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees})
			declineStep = nil
			if err == nil {
				t.Fatal("Decline() was not cut short")
			}
			if m := readCampManifest(t, root); !m.Providers["festival-app"].Declined {
				t.Fatalf("cut short after %s: providers = %+v, want declined already recorded", step, m.Providers)
			}
			hadFolder := fileExists(filepath.Join(skillsDir, "festival-app"))
			res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Auto: true})
			if res.Files[0].State != ShippedDeclined || fileExists(filepath.Join(skillsDir, "festival-app")) != hadFolder {
				t.Fatalf("cut short after %s: an automatic request acted on it: %+v", step, res.Files[0])
			}
			if _, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees}); err != nil {
				t.Fatalf("cut short after %s: second Decline() error = %v", step, err)
			}
			final := readCampManifest(t, root)
			if fileExists(filepath.Join(skillsDir, "festival-app")) || final.Bundles["festival-app"] != nil || !final.Providers["festival-app"].Declined {
				t.Fatalf("cut short after %s: second decline did not finish: %+v %v", step, final.Providers, final.Bundles["festival-app"])
			}
		})
	}
}

// decline removes only inside the camp. A tool folder or a worktrees folder
// that is a symlink out of the camp is left alone, with the link into the
// bundle it holds.
func TestDeclineLeavesFoldersOutsideTheCampAlone(t *testing.T) {
	root, worktrees := linkedPluginCamp(t)
	bundle := filepath.Join(root, ".campaign", "skills", "festival-app")
	outsideTool, outsideWT := t.TempDir(), t.TempDir()

	if err := os.MkdirAll(filepath.Join(outsideTool, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bundle, filepath.Join(outsideTool, "skills", "festival-app")); err != nil {
		t.Fatal(err)
	}
	removeAll(t, filepath.Join(root, ".claude"))
	if err := os.Symlink(outsideTool, filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}

	wtSkills := filepath.Join(outsideWT, "app", "feature", ".agents", "skills")
	if err := os.MkdirAll(wtSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outsideWT, "app", "feature", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bundle, filepath.Join(wtSkills, "festival-app")); err != nil {
		t.Fatal(err)
	}
	removeAll(t, worktrees)
	if err := os.Symlink(outsideWT, worktrees); err != nil {
		t.Fatal(err)
	}

	res, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, WorktreesRoot: worktrees})
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	for _, outside := range []string{filepath.Join(outsideTool, "skills", "festival-app"), filepath.Join(wtSkills, "festival-app")} {
		if _, err := os.Lstat(outside); err != nil {
			t.Errorf("decline removed %s outside the camp: %v", outside, err)
		}
	}
	if !slices.Equal(res.LinksRemoved, []string{".agents/skills/festival-app"}) {
		t.Fatalf("LinksRemoved = %v, want only the link inside the camp", res.LinksRemoved)
	}
}

// A forced decline that fails after it backed up the edited files still
// reports how to restore them.
func TestDeclineFailureAfterTheBackupKeepsTheRestore(t *testing.T) {
	root, _ := linkedPluginCamp(t)
	bundle := filepath.Join(root, ".campaign", "skills", "festival-app")
	if err := os.WriteFile(filepath.Join(bundle, "notes.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	declineStep = func(at string) error {
		if at == "folder" {
			return errors.New("cut short")
		}
		return nil
	}
	defer func() { declineStep = nil }()
	res, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin, Force: true})
	if err == nil || res == nil || res.Restore == nil || res.Restore.BackupDir == "" {
		t.Fatalf("Decline() = %+v, %v; want the failure with the restore command", res, err)
	}
	if !fileExists(filepath.Join(root, filepath.FromSlash(res.Restore.BackupDir), "notes.md")) {
		t.Fatal("the backup named in the restore command is missing")
	}
}
