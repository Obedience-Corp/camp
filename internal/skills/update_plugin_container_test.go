//go:build container_fs

package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func campShipped() []ShippedFile {
	return []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}
}

func readSkill(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".campaign", "skills", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readCampManifest(t *testing.T, root string) *Manifest {
	t.Helper()
	m, err := ReadManifest(os.DirFS(filepath.Join(root, ".campaign", "skills")))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func updatePlugin(t *testing.T, root string, offer PluginBundle, opts UpdateOptions) *UpdateResult {
	t.Helper()
	opts.Root, opts.Shipped, opts.Plugins, opts.Slugs, opts.CampVersion = root, campShipped(), []PluginBundle{offer}, []string{offer.Name}, "v2"
	res, err := Update(t.Context(), opts)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	return res
}

func TestUpdatePluginBundleAddsThenUpdatesAndRecordsTheWriter(t *testing.T) {
	root := t.TempDir()
	res := updatePlugin(t, root, offered("0.3.0", oldSkill), UpdateOptions{})
	if got := readSkill(t, root, "festival-app/SKILL.md"); got != oldSkill || !res.ManifestWritten {
		t.Fatalf("added content = %q (manifest %v)", got, res.ManifestWritten)
	}
	if fileExists(filepath.Join(root, ".campaign", "skills", "alpha")) {
		t.Fatal("a named plugin bundle refreshed camp's own bundle")
	}
	updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{})
	m := readCampManifest(t, root)
	if readSkill(t, root, "festival-app/SKILL.md") != newSkill || m.Providers["festival-app"] != (ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"}) {
		t.Fatalf("after update: providers = %+v", m.Providers)
	}
	again := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{})
	if again.ManifestWritten || again.Files[0].Written {
		t.Fatal("a current plugin bundle was written again")
	}
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestUpdatePluginBundlePreservesEditAndForceBacksUp(t *testing.T) {
	root := t.TempDir()
	updatePlugin(t, root, offered("0.3.0", oldSkill), UpdateOptions{})
	edited := filepath.Join(root, ".campaign", "skills", "festival-app", "SKILL.md")
	if err := os.WriteFile(edited, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Auto: true})
	if res.Files[0].State != ShippedModified || readSkill(t, root, "festival-app/SKILL.md") != "mine\n" {
		t.Fatalf("automatic update touched an edit: state %s", res.Files[0].State)
	}
	res = updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Force: true})
	if len(res.Undo) != 1 || res.Undo[0].BackupDir == "" || readSkill(t, root, "festival-app/SKILL.md") != newSkill {
		t.Fatalf("forced update: undo %+v", res.Undo)
	}
	backup, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res.Undo[0].BackupDir), "SKILL.md"))
	if err != nil || string(backup) != "mine\n" {
		t.Fatalf("backup = %q, %v", backup, err)
	}
}

func TestUpdatePluginBundleAheadWritesNothingEvenForced(t *testing.T) {
	root := t.TempDir()
	updatePlugin(t, root, offered("0.5.0", oldSkill), UpdateOptions{})
	res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Force: true})
	if res.Files[0].State != ShippedAhead || res.Files[0].Written || readSkill(t, root, "festival-app/SKILL.md") != oldSkill {
		t.Fatalf("older plugin overwrote a newer bundle: %+v", res.Files[0])
	}
	if m := readCampManifest(t, root); m.Providers["festival-app"].Version != "0.5.0" {
		t.Fatalf("recorded version = %q, want 0.5.0 kept", m.Providers["festival-app"].Version)
	}
}

func TestUpdatePluginBundleAutoLeavesDeclinedAlone(t *testing.T) {
	root := t.TempDir()
	updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{})
	if _, err := Decline(t.Context(), DeclineOptions{Root: root, Slug: "festival-app", Plugin: CampAppPlugin}); err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Auto: true})
	if res.Files[0].State != ShippedDeclined || res.ManifestWritten || fileExists(filepath.Join(root, ".campaign", "skills", "festival-app")) {
		t.Fatalf("automatic request re-added a declined bundle: %+v", res.Files[0])
	}
	updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{})
	if readSkill(t, root, "festival-app/SKILL.md") != newSkill || readCampManifest(t, root).Providers["festival-app"].Declined {
		t.Fatal("naming a declined bundle did not add it back and clear the decline")
	}
}

func TestUpdatePluginBundleNeverWritesThroughASymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	skillsDir := filepath.Join(root, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillsDir, "festival-app")); err != nil {
		t.Fatal(err)
	}
	res := updatePlugin(t, root, offered("0.4.0", newSkill), UpdateOptions{Force: true})
	if fileExists(filepath.Join(outside, "SKILL.md")) || res.Files[0].Written {
		t.Fatal("a plugin file was written through a symlinked bundle folder")
	}
}

// Something odd at a plugin bundle's path is reported and left alone, never a
// failure, and a camp bundle named in the same run is still refreshed.
func TestUpdatePluginBundleWithAnOddShapeIsKept(t *testing.T) {
	for name, stage := range map[string]func(t *testing.T, skillsDir string){
		"bundle is a file": func(t *testing.T, skillsDir string) {
			if err := os.WriteFile(filepath.Join(skillsDir, "festival-app"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"SKILL.md is a folder": func(t *testing.T, skillsDir string) {
			if err := os.MkdirAll(filepath.Join(skillsDir, "festival-app", "SKILL.md"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	} {
		for _, force := range []bool{false, true} {
			root := t.TempDir()
			skillsDir := filepath.Join(root, ".campaign", "skills")
			if err := os.MkdirAll(skillsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			stage(t, skillsDir)
			res, err := Update(t.Context(), UpdateOptions{
				Root: root, Shipped: campShipped(), Plugins: []PluginBundle{offered("0.4.0", newSkill)},
				Slugs: []string{"alpha", "festival-app"}, Force: force, CampVersion: "v2",
			})
			if err != nil {
				t.Fatalf("%s force=%v: Update() error = %v, want the odd bundle kept", name, force, err)
			}
			var plugin FileOutcome
			for _, f := range res.Files {
				if f.Bundle == "festival-app" {
					plugin = f
				}
			}
			if plugin.State != ShippedUnknown || plugin.Action != ActionPreserve || plugin.Written || plugin.Problem == "" {
				t.Fatalf("%s force=%v: plugin outcome = %+v, want unknown, preserved, with a problem", name, force, plugin)
			}
			if readSkill(t, root, "alpha/SKILL.md") != "alpha v2\n" {
				t.Fatalf("%s: the camp bundle named in the same run was not refreshed", name)
			}
		}
	}
}

func TestUpdateWithPluginsButNoSlugsRefreshesOnlyCampBundles(t *testing.T) {
	root := t.TempDir()
	res, err := Update(t.Context(), UpdateOptions{Root: root, Shipped: campShipped(), Plugins: []PluginBundle{offered("0.4.0", newSkill)}, CampVersion: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(root, ".campaign", "skills", "festival-app")) || len(res.Files) != 1 {
		t.Fatalf("an unnamed plugin bundle was written: %+v", res.Files)
	}
}
