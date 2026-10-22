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
