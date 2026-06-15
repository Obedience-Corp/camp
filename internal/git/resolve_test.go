package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveTarget_Default(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a git repo
	initGitRepo(t, tmpDir)

	result, err := ResolveTarget(ctx, tmpDir, false, "")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}

	if result.Path != tmpDir {
		t.Errorf("Path = %q, want %q", result.Path, tmpDir)
	}
	if result.IsSubmodule {
		t.Error("IsSubmodule should be false for campaign root")
	}
	if result.Name != "campaign root" {
		t.Errorf("Name = %q, want %q", result.Name, "campaign root")
	}
}

func TestResolveTarget_ExplicitProject(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create campaign root repo
	initGitRepo(t, tmpDir)

	// Create a project directory with its own git repo
	projectDir := filepath.Join(tmpDir, "projects", "myproject")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, projectDir)

	result, err := ResolveTarget(ctx, tmpDir, false, "projects/myproject")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}

	if result.Path != projectDir {
		t.Errorf("Path = %q, want %q", result.Path, projectDir)
	}
	if result.Name != "myproject" {
		t.Errorf("Name = %q, want %q", result.Name, "myproject")
	}
}

func TestResolveTarget_InvalidProject(t *testing.T) {
	ctx := context.Background()
	// Use a fresh temp dir with no git repo anywhere in its hierarchy
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create a directory that is NOT a git repo and is isolated
	isolatedDir := filepath.Join(tmpDir, "isolated")
	if err := os.MkdirAll(isolatedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Point to a path that doesn't exist at all
	_, err := ResolveTarget(ctx, isolatedDir, false, "/nonexistent/absolute/path/that/does/not/exist")
	if err == nil {
		t.Error("ResolveTarget() should error for nonexistent absolute project path")
	}
}

func TestResolveTarget_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveTarget(ctx, "/tmp/fake", false, "")
	if err == nil {
		t.Error("ResolveTarget() should error when context is cancelled")
	}
}

func TestResolveTarget_SymlinkedCampaignRoot(t *testing.T) {
	ctx := context.Background()
	realRoot := t.TempDir()
	initGitRepo(t, realRoot)

	linkRoot := filepath.Join(t.TempDir(), "campaign-link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(realRoot); err != nil {
		t.Fatal(err)
	}

	result, err := ResolveTarget(ctx, linkRoot, true, "")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if result.IsSubmodule {
		t.Fatal("ResolveTarget() treated symlinked campaign root as submodule")
	}
}

func TestExtractSubFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantRemaining []string
		wantSub       bool
		wantProject   string
	}{
		{
			name:          "no camp flags",
			args:          []string{"--oneline", "-5"},
			wantRemaining: []string{"--oneline", "-5"},
		},
		{
			name:          "sub flag extracted",
			args:          []string{"--sub", "--oneline"},
			wantRemaining: []string{"--oneline"},
			wantSub:       true,
		},
		{
			name:          "bare -p passes through to git untouched",
			args:          []string{"-p", "origin"},
			wantRemaining: []string{"-p", "origin"},
		},
		{
			name:          "bare -p at end passes through to git untouched",
			args:          []string{"-p"},
			wantRemaining: []string{"-p"},
		},
		{
			name:          "project equals form extracted",
			args:          []string{"--project=projects/camp", "origin"},
			wantRemaining: []string{"origin"},
			wantProject:   "projects/camp",
		},
		{
			name:          "project space form extracted",
			args:          []string{"--project", "projects/camp", "origin"},
			wantRemaining: []string{"origin"},
			wantProject:   "projects/camp",
		},
		{
			name:        "sub and project long flags extracted",
			args:        []string{"--sub", "--project", "projects/camp"},
			wantSub:     true,
			wantProject: "projects/camp",
		},
		{
			name:          "terminator stops camp flag extraction",
			args:          []string{"--", "-p", "origin"},
			wantRemaining: []string{"--", "-p", "origin"},
		},
		{
			name:          "terminator preserves later camp-looking flags",
			args:          []string{"--sub", "--", "--project=projects/camp", "-p", "origin"},
			wantRemaining: []string{"--", "--project=projects/camp", "-p", "origin"},
			wantSub:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remaining, sub, project := ExtractSubFlags(tt.args)

			if !slices.Equal(remaining, tt.wantRemaining) {
				t.Errorf("remaining = %v, want %v", remaining, tt.wantRemaining)
			}
			if sub != tt.wantSub {
				t.Errorf("sub = %v, want %v", sub, tt.wantSub)
			}
			if project != tt.wantProject {
				t.Errorf("project = %q, want %q", project, tt.wantProject)
			}
		})
	}
}

func TestHasPullStrategyFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nil args", nil, false},
		{"empty args", []string{}, false},
		{"unrelated flags", []string{"--verbose", "-q"}, false},
		{"rebase", []string{"--rebase"}, true},
		{"no-rebase", []string{"--no-rebase"}, true},
		{"ff-only", []string{"--ff-only"}, true},
		{"ff", []string{"--ff"}, true},
		{"no-ff", []string{"--no-ff"}, true},
		{"strategy mixed with others", []string{"--verbose", "--rebase", "-q"}, true},
		{"prefix should not match", []string{"--rebase-merges"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPullStrategyFlag(tt.args); got != tt.want {
				t.Errorf("HasPullStrategyFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+t.TempDir(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
}
