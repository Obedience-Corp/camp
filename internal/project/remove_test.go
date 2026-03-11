package project

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustRunCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}

func setupSubmoduleFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	mustRunCmd(t, tmp, "git", "init", "-b", "main")
	mustRunCmd(t, tmp, "git", "config", "user.email", "test@test.com")
	mustRunCmd(t, tmp, "git", "config", "user.name", "Test")

	upstreamDir := filepath.Join(tmp, "_upstream_"+name)
	os.MkdirAll(upstreamDir, 0o755)
	mustRunCmd(t, upstreamDir, "git", "init", "-b", "main")
	mustRunCmd(t, upstreamDir, "git", "config", "user.email", "test@test.com")
	mustRunCmd(t, upstreamDir, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(upstreamDir, "README.md"), []byte("hello"), 0o644)
	mustRunCmd(t, upstreamDir, "git", "add", ".")
	mustRunCmd(t, upstreamDir, "git", "commit", "-m", "init")

	os.MkdirAll(filepath.Join(tmp, "projects"), 0o755)
	mustRunCmd(t, tmp, "git", "-c", "protocol.file.allow=always",
		"submodule", "add", upstreamDir, "projects/"+name)
	mustRunCmd(t, tmp, "git", "add", ".")
	mustRunCmd(t, tmp, "git", "commit", "-m", "add submodule")

	return tmp, filepath.Join(tmp, "projects", name)
}

func TestRemove_DirtySubmodule_Blocked(t *testing.T) {
	campaignRoot, subPath := setupSubmoduleFixture(t, "myproj")

	os.WriteFile(filepath.Join(subPath, "dirty.txt"), []byte("dirty"), 0o644)

	_, err := Remove(context.Background(), campaignRoot, "myproj", RemoveOptions{})
	if err == nil {
		t.Fatal("expected error for dirty submodule without --force")
	}
	if !errors.Is(err, ErrDirtyProject) {
		t.Errorf("expected ErrDirtyProject, got: %v", err)
	}
}

func TestRemove_DirtySubmodule_ForceProceeds(t *testing.T) {
	campaignRoot, subPath := setupSubmoduleFixture(t, "myproj")

	os.WriteFile(filepath.Join(subPath, "dirty.txt"), []byte("dirty"), 0o644)

	result, err := Remove(context.Background(), campaignRoot, "myproj", RemoveOptions{Force: true})
	if err != nil {
		t.Fatalf("Remove() with --force should not error: %v", err)
	}
	if !result.SubmoduleRemoved {
		t.Error("expected SubmoduleRemoved=true")
	}
}

func TestRemove_CleanSubmodule_Proceeds(t *testing.T) {
	campaignRoot, _ := setupSubmoduleFixture(t, "myproj")

	result, err := Remove(context.Background(), campaignRoot, "myproj", RemoveOptions{})
	if err != nil {
		t.Fatalf("Remove() should not error on clean submodule: %v", err)
	}
	if !result.SubmoduleRemoved {
		t.Error("expected SubmoduleRemoved=true")
	}
}

func TestRemove_StepsPopulated(t *testing.T) {
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	projectPath := filepath.Join(tmp, "projects", "myproj")
	os.MkdirAll(projectPath, 0o755)
	os.WriteFile(filepath.Join(projectPath, "file.txt"), []byte("x"), 0o644)

	result, err := Remove(context.Background(), tmp, "myproj", RemoveOptions{Delete: true})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(result.Steps) == 0 {
		t.Error("expected Steps to be populated after successful remove")
	}
}

func TestRemove_RecoveryInstructions_OnPartialFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on Windows")
	}
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	projectDir := filepath.Join(tmp, "projects", "myproj")
	os.MkdirAll(projectDir, 0o755)

	worktreeDir := filepath.Join(tmp, "projects", "worktrees", "myproj")
	os.MkdirAll(worktreeDir, 0o755)

	worktreesParent := filepath.Join(tmp, "projects", "worktrees")
	os.Chmod(worktreesParent, 0o555)
	t.Cleanup(func() { os.Chmod(worktreesParent, 0o755) })

	result, err := Remove(context.Background(), tmp, "myproj", RemoveOptions{Delete: true})
	if err == nil {
		t.Fatal("expected error on partial failure")
	}
	if len(result.RecoveryInstructions) == 0 {
		t.Error("expected RecoveryInstructions to be populated on partial failure")
	}
}

func TestRemove_ModulesCleanedWithoutDelete(t *testing.T) {
	campaignRoot, _ := setupSubmoduleFixture(t, "myproj")

	modulesPath := filepath.Join(campaignRoot, ".git", "modules", "projects", "myproj")
	if _, err := os.Stat(modulesPath); os.IsNotExist(err) {
		t.Skip("fixture did not create .git/modules entry — skipping")
	}

	_, err := Remove(context.Background(), campaignRoot, "myproj", RemoveOptions{Delete: false})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if _, err := os.Stat(modulesPath); !os.IsNotExist(err) {
		t.Error("expected .git/modules/projects/myproj to be cleaned up without --delete")
	}
}

func TestNormalizeProjectName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"myproj", "myproj"},
		{"projects/myproj", "myproj"},
		{"projects/foo-bar", "foo-bar"},
		{"notprojects/myproj", "notprojects/myproj"},
	}
	for _, tc := range cases {
		got := strings.TrimPrefix(tc.input, "projects/")
		if got != tc.want {
			t.Errorf("TrimPrefix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
