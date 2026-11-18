//go:build container_fs

package skills

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func twinShipped() []ShippedFile {
	return []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
		{Bundle: "gamma", Path: "SKILL.md", Content: []byte("gamma v2\n")},
	}
}

var twinPluginProviders = map[string]ProviderRecord{
	"festival-app": {Plugin: "camp-app", Version: "0.4.0"},
	"other-app":    {Plugin: "camp-app", Declined: true},
}

// stageTwinCamp writes a camp with one current, one outdated and one missing
// camp bundle. withPlugins adds a plugin bundle and a declined bundle on top.
func stageTwinCamp(t *testing.T, withPlugins bool) string {
	t.Helper()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".campaign", "skills")
	for _, f := range []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v1\n")},
	} {
		if err := writeShipped(skillsDir, f); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manifest{SchemaVersion: ManifestSchemaVersion, CampVersion: "v1"}
	m.set("alpha", "SKILL.md", HashContent([]byte("alpha v2\n")))
	m.set("beta", "SKILL.md", HashContent([]byte("beta v1\n")))
	if withPlugins {
		if err := writeShipped(skillsDir, ShippedFile{Bundle: "festival-app", Path: "SKILL.md", Content: []byte(pluginSkill)}); err != nil {
			t.Fatal(err)
		}
		m.set("festival-app", "SKILL.md", HashContent([]byte(pluginSkill)))
		m.Providers = maps.Clone(twinPluginProviders)
	}
	if err := writeManifest(skillsDir, m); err != nil {
		t.Fatal(err)
	}
	return root
}

func skillTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	skillsDir := filepath.Join(root, ".campaign", "skills")
	err := filepath.WalkDir(skillsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(skillsDir, p)
		data, err := os.ReadFile(p)
		tree[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func writtenPaths(res *UpdateResult) []string {
	var paths []string
	for _, f := range res.Files {
		if f.Written {
			paths = append(paths, f.RelPath())
		}
	}
	return paths
}

// Plain update is the path every existing camp runs. With plugin and declined
// records in its manifest it must write exactly what a twin camp without them
// writes, and keep those records.
func TestUpdateWithoutPluginsKeepsPluginRecordsAndWritesLikeATwin(t *testing.T) {
	stamp := func() time.Time { return time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC) }
	withPlugins, twin := stageTwinCamp(t, true), stageTwinCamp(t, false)

	got, err := Update(t.Context(), UpdateOptions{Root: withPlugins, Shipped: twinShipped(), CampVersion: "v2", Now: stamp})
	if err != nil {
		t.Fatalf("Update() with plugin records error = %v", err)
	}
	want, err := Update(t.Context(), UpdateOptions{Root: twin, Shipped: twinShipped(), CampVersion: "v2", Now: stamp})
	if err != nil {
		t.Fatalf("Update() of the twin error = %v", err)
	}
	if !slices.Equal(writtenPaths(got), writtenPaths(want)) || got.ManifestWritten != want.ManifestWritten {
		t.Fatalf("written = %v (manifest %v), twin wrote %v (manifest %v)",
			writtenPaths(got), got.ManifestWritten, writtenPaths(want), want.ManifestWritten)
	}

	gotTree, wantTree := skillTree(t, withPlugins), skillTree(t, twin)
	if gotTree["festival-app/SKILL.md"] != pluginSkill {
		t.Fatalf("plugin bundle changed: %q", gotTree["festival-app/SKILL.md"])
	}
	delete(gotTree, "festival-app/SKILL.md")
	delete(gotTree, ManifestFileName)
	delete(wantTree, ManifestFileName)
	if !maps.Equal(gotTree, wantTree) {
		t.Fatalf("files differ from the twin:\n got: %v\nwant: %v", gotTree, wantTree)
	}

	gotManifest, err := ReadManifest(os.DirFS(filepath.Join(withPlugins, ".campaign", "skills")))
	if err != nil {
		t.Fatal(err)
	}
	wantManifest, err := ReadManifest(os.DirFS(filepath.Join(twin, ".campaign", "skills")))
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(gotManifest.Providers, twinPluginProviders) {
		t.Fatalf("Providers = %v, want %v", gotManifest.Providers, twinPluginProviders)
	}
	if gotManifest.Bundles["festival-app"]["SKILL.md"] != HashContent([]byte(pluginSkill)) {
		t.Fatalf("plugin bundle record lost: %v", gotManifest.Bundles)
	}
	delete(gotManifest.Bundles, "festival-app")
	gotManifest.Providers = nil
	if !gotManifest.sameRecords(wantManifest) || gotManifest.CampVersion != wantManifest.CampVersion {
		t.Fatalf("camp records differ from the twin:\n got: %+v\nwant: %+v", gotManifest, wantManifest)
	}

	again, err := Update(t.Context(), UpdateOptions{Root: withPlugins, Shipped: twinShipped(), CampVersion: "v2", Now: stamp})
	if err != nil {
		t.Fatalf("second Update() error = %v", err)
	}
	if len(writtenPaths(again)) != 0 || again.ManifestWritten {
		t.Fatalf("second Update() wrote %v (manifest %v), want nothing", writtenPaths(again), again.ManifestWritten)
	}
}
