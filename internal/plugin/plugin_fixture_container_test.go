//go:build container_fs

package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLookup_Found(t *testing.T) {
	dir := t.TempDir()
	binName := Prefix + "testplugin"
	binPath := filepath.Join(dir, binName)

	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p, found := Lookup("testplugin")
	if !found {
		t.Fatal("expected Lookup to find camp-testplugin")
	}
	if p.Name != "testplugin" {
		t.Errorf("Name = %q, want %q", p.Name, "testplugin")
	}
	if p.Path == "" {
		t.Error("Path should not be empty")
	}
}

func TestDiscover_FindsPlugins(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"camp-alpha", "camp-beta"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	nonPlugin := filepath.Join(dir, "other-tool")
	if err := os.WriteFile(nonPlugin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)

	plugins, err := Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want 2", len(plugins))
	}

	names := map[string]bool{}
	for _, p := range plugins {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestDiscover_Deduplicates(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	for _, dir := range []string{dir1, dir2} {
		path := filepath.Join(dir, "camp-dupe")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("PATH", dir1+string(os.PathListSeparator)+dir2)

	plugins, err := Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("got %d plugins, want 1 (deduplication)", len(plugins))
	}

	if got := filepath.Dir(plugins[0].Path); got != dir1 {
		t.Errorf("expected plugin from %s, got %s", dir1, plugins[0].Path)
	}
}

func TestDiscover_SkipsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable permission check not applicable on Windows")
	}

	dir := t.TempDir()

	path := filepath.Join(dir, "camp-noexec")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)

	plugins, err := Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(plugins) != 0 {
		t.Errorf("got %d plugins, want 0 (non-executable should be skipped)", len(plugins))
	}
}

func TestExecute_Success(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true command not found")
	}

	p := Plugin{Name: "test", Path: truePath}
	err = Execute(context.Background(), p, nil, "/tmp/test-root")
	if err != nil {
		t.Errorf("Execute returned error for exit-0 binary: %v", err)
	}
}

func TestExecute_NonZeroExit(t *testing.T) {
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false command not found")
	}

	p := Plugin{Name: "test", Path: falsePath}
	err = Execute(context.Background(), p, nil, "")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}
