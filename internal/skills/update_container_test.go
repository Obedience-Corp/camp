//go:build container_fs

package skills

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateCancelStopsAfterFileInFlight(t *testing.T) {
	root := t.TempDir()
	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta\n")},
		{Bundle: "gamma", Path: "SKILL.md", Content: []byte("gamma\n")},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	res, err := Update(ctx, UpdateOptions{
		Root:        root,
		Shipped:     shipped,
		CampVersion: "test",
		OnWrite:     func(FileOutcome) { cancel() },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Update() error = %v, want context.Canceled", err)
	}
	if res == nil || res.ManifestWritten {
		t.Fatalf("Update() result = %+v, want a partial result without a manifest", res)
	}
	var reported []string
	for _, f := range res.Files {
		if f.Written {
			reported = append(reported, f.RelPath())
		}
	}
	if len(reported) != 1 || reported[0] != ".campaign/skills/alpha/SKILL.md" {
		t.Fatalf("written files reported = %v, want only the file in flight", reported)
	}

	var onDisk []string
	skillsDir := filepath.Join(root, ".campaign", "skills")
	err = filepath.WalkDir(skillsDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(skillsDir, p)
			onDisk = append(onDisk, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 1 || onDisk[0] != "alpha/SKILL.md" {
		t.Fatalf("files on disk after cancel = %v, want only alpha/SKILL.md (no manifest, no temp files)", onDisk)
	}
}

func TestUpdateForceOutsideGitBacksUpBeforeReplacing(t *testing.T) {
	root := t.TempDir()
	shipped := []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("shipped\n")}}
	if err := writeShipped(filepath.Join(root, ".campaign", "skills"), ShippedFile{Bundle: "alpha", Path: "SKILL.md", Content: []byte("mine\n")}); err != nil {
		t.Fatal(err)
	}

	res, err := Update(t.Context(), UpdateOptions{Root: root, Shipped: shipped, Force: true, CampVersion: "test"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(res.Undo) != 1 || res.Undo[0].BackupDir == "" {
		t.Fatalf("Undo = %+v, want one backup outside git", res.Undo)
	}
	backup, err := fs.ReadFile(os.DirFS(root), res.Undo[0].BackupDir+"/SKILL.md")
	if err != nil || string(backup) != "mine\n" {
		t.Fatalf("backup = %q, %v; want the replaced local content", backup, err)
	}
	live, err := fs.ReadFile(os.DirFS(root), ".campaign/skills/alpha/SKILL.md")
	if err != nil || string(live) != "shipped\n" {
		t.Fatalf("live file = %q, %v; want the shipped content", live, err)
	}
}

func TestUpdateLeavesSymlinkedFilesAlone(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".campaign", "skills")
	shared := filepath.Join(root, "shared")
	for _, dir := range []string{filepath.Join(skillsDir, "alpha"), filepath.Join(shared, "beta")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stale := []byte("alpha v1\n")
	if err := os.WriteFile(filepath.Join(shared, "alpha.md"), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "beta", "SKILL.md"), []byte("beta v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../shared/alpha.md", filepath.Join(skillsDir, "alpha", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../shared/beta", filepath.Join(skillsDir, "beta")); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{SchemaVersion: ManifestSchemaVersion, CampVersion: "v1"}
	manifest.set("alpha", "SKILL.md", HashContent(stale))
	if err := writeManifest(skillsDir, manifest); err != nil {
		t.Fatal(err)
	}

	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
	}
	res, err := Update(t.Context(), UpdateOptions{Root: root, Shipped: shipped, Force: true, CampVersion: "v2"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	for _, f := range res.Files {
		if f.State != ShippedSymlink || f.Action != ActionPreserve || f.Written {
			t.Errorf("%s = state %q action %q written %v; want a preserved symlink", f.RelPath(), f.State, f.Action, f.Written)
		}
	}
	if len(res.Undo) != 0 {
		t.Errorf("Undo = %+v, want none: nothing was replaced", res.Undo)
	}
	for _, link := range []string{filepath.Join(skillsDir, "alpha", "SKILL.md"), filepath.Join(skillsDir, "beta")} {
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symlink (err %v)", link, err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(shared, "alpha.md"):         "alpha v1\n",
		filepath.Join(shared, "beta", "SKILL.md"): "beta v1\n",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("shared target %s = %q, %v; want it untouched", path, got, err)
		}
	}
}
