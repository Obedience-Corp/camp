package clone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseSubmoduleStatus(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantSuccess bool
		wantCommit  string
		wantPath    string
		wantError   bool
	}{
		{
			name:        "initialized submodule",
			line:        " abc123def456789 projects/sub (v1.0.0)",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "uninitialized submodule",
			line:        "-abc123def456789 projects/sub",
			wantSuccess: false,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   true,
		},
		{
			name:        "commit mismatch (prefix +)",
			line:        "+abc123def456789 projects/sub (heads/main)",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "no prefix",
			line:        "abc123def456789 projects/sub",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "empty line",
			line:        "",
			wantSuccess: false,
			wantCommit:  "",
			wantPath:    "",
			wantError:   false,
		},
		{
			name:        "nested submodule path",
			line:        " fedcba987654321 projects/nested/deep/sub (v2.0.0-rc1)",
			wantSuccess: true,
			wantCommit:  "fedcba987654321",
			wantPath:    "projects/nested/deep/sub",
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSubmoduleStatus(tt.line)

			if result.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}
			if result.Commit != tt.wantCommit {
				t.Errorf("Commit = %q, want %q", result.Commit, tt.wantCommit)
			}
			if result.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", result.Path, tt.wantPath)
			}
			if tt.wantError && result.Error == nil {
				t.Error("Error = nil, want error")
			}
			if !tt.wantError && result.Error != nil {
				t.Errorf("Error = %v, want nil", result.Error)
			}
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "https URL with .git",
			url:      "https://github.com/org/repo.git",
			expected: "repo",
		},
		{
			name:     "https URL without .git",
			url:      "https://github.com/org/repo",
			expected: "repo",
		},
		{
			name:     "ssh URL with colon",
			url:      "git@github.com:org/repo.git",
			expected: "repo",
		},
		{
			name:     "ssh:// URL",
			url:      "ssh://git@github.com/org/repo.git",
			expected: "repo",
		},
		{
			name:     "trailing slash",
			url:      "https://github.com/org/repo/",
			expected: "repo",
		},
		{
			name:     "trailing slash with .git",
			url:      "https://github.com/org/repo.git/",
			expected: "repo",
		},
		{
			name:     "just repo name",
			url:      "repo.git",
			expected: "repo",
		},
		{
			name:     "simple path",
			url:      "/path/to/repo.git",
			expected: "repo",
		},
		{
			name:     "gitlab style",
			url:      "https://gitlab.com/group/subgroup/repo.git",
			expected: "repo",
		},
		{
			name:     "bitbucket style",
			url:      "git@bitbucket.org:team/repo.git",
			expected: "repo",
		},
		{
			name:     "repo name with dots",
			url:      "https://github.com/org/my.dotted.repo.git",
			expected: "my.dotted.repo",
		},
		{
			name:     "repo name with hyphens",
			url:      "https://github.com/org/my-hyphenated-repo.git",
			expected: "my-hyphenated-repo",
		},
		{
			name:     "empty string",
			url:      "",
			expected: ".", // filepath.Base("") returns "."
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRepoName(tt.url)
			if result != tt.expected {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

// Integration tests that require actual git operations

func TestGitClone_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a source repo to clone from
	sourceDir := setupTestRepo(t)

	// Create cloner targeting a temp directory
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true), // No submodules in test repo
	)

	// Perform the clone
	dir, err := c.gitClone(ctx)
	if err != nil {
		t.Fatalf("gitClone() error = %v", err)
	}

	// Verify the clone exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("cloned directory does not exist: %s", dir)
	}

	// Verify it's a git repo
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf("cloned directory is not a git repo (no .git): %s", dir)
	}
}

func TestGitClone_WithBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a source repo with a specific branch
	sourceDir := setupTestRepo(t)
	runGit(t, sourceDir, "checkout", "-b", "test-branch")
	createFile(t, filepath.Join(sourceDir, "branch-file.txt"), "branch content")
	runGit(t, sourceDir, "add", ".")
	runGit(t, sourceDir, "commit", "-m", "Branch commit")

	// Create cloner targeting the specific branch
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithBranch("test-branch"),
		WithNoSubmodules(true),
	)

	dir, err := c.gitClone(ctx)
	if err != nil {
		t.Fatalf("gitClone() error = %v", err)
	}

	// Verify the branch
	branch, err := c.gitGetBranch(ctx, dir)
	if err != nil {
		t.Fatalf("gitGetBranch() error = %v", err)
	}
	if branch != "test-branch" {
		t.Errorf("branch = %q, want %q", branch, "test-branch")
	}
}

func TestGitGetBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	c := NewCloner()
	branch, err := c.gitGetBranch(ctx, repoDir)
	if err != nil {
		t.Fatalf("gitGetBranch() error = %v", err)
	}

	// Default branch could be "master" or "main" depending on git config
	if branch != "master" && branch != "main" {
		t.Errorf("gitGetBranch() = %q, want 'master' or 'main'", branch)
	}
}

func TestGitSubmoduleStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub1")
	setupSubmodule(t, repoDir, "projects/sub2")

	c := NewCloner()
	results, err := c.gitSubmoduleStatus(ctx, repoDir)
	if err != nil {
		t.Fatalf("gitSubmoduleStatus() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("gitSubmoduleStatus() returned %d results, want 2", len(results))
	}

	// Verify submodule paths
	paths := make(map[string]bool)
	for _, r := range results {
		paths[r.Path] = true
	}

	if !paths["projects/sub1"] {
		t.Error("expected projects/sub1 in results")
	}
	if !paths["projects/sub2"] {
		t.Error("expected projects/sub2 in results")
	}
}

func TestGitSubmoduleURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	c := NewCloner()
	url, err := c.gitSubmoduleURL(ctx, repoDir, "projects/sub")
	if err != nil {
		t.Fatalf("gitSubmoduleURL() error = %v", err)
	}

	// URL should be non-empty (it's a local path in tests)
	if url == "" {
		t.Error("gitSubmoduleURL() returned empty URL")
	}
}

func TestGitClone_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner(WithURL("https://github.com/test/repo.git"))

	_, err := c.gitClone(ctx)
	if err != context.Canceled {
		t.Errorf("gitClone() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleSync_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	err := c.gitSubmoduleSync(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleSync() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleUpdate_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	err := c.gitSubmoduleUpdate(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleUpdate() error = %v, want context.Canceled", err)
	}
}

func TestGitGetBranch_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	_, err := c.gitGetBranch(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitGetBranch() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleStatus_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	_, err := c.gitSubmoduleStatus(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleStatus() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleSync_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	c := NewCloner()
	err := c.gitSubmoduleSync(ctx, repoDir)
	if err != nil {
		t.Fatalf("gitSubmoduleSync() error = %v", err)
	}
}

func TestGitSubmoduleUpdate_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	c := NewCloner()
	err := c.gitSubmoduleUpdate(ctx, repoDir)
	if err != nil {
		t.Fatalf("gitSubmoduleUpdate() error = %v", err)
	}
}

func TestGitSubmoduleURL_Fallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	// Remove the URL from .gitmodules to test fallback to .git/config
	runGit(t, repoDir, "config", "-f", ".gitmodules", "--remove-section", "submodule.projects/sub")

	c := NewCloner()
	url, err := c.gitSubmoduleURL(ctx, repoDir, "projects/sub")
	if err != nil {
		t.Fatalf("gitSubmoduleURL() error = %v", err)
	}

	// URL should still be retrievable from .git/config
	if url == "" {
		t.Error("gitSubmoduleURL() returned empty URL")
	}
}

func TestGitSubmoduleURL_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	c := NewCloner()
	_, err := c.gitSubmoduleURL(ctx, repoDir, "nonexistent/submodule")
	if err == nil {
		t.Error("gitSubmoduleURL() error = nil, want error for nonexistent submodule")
	}
}

func TestGitSubmoduleURL_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()
	_, err := c.gitSubmoduleURL(ctx, "/tmp/fake", "sub")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleURL() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleSync_InvalidDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	c := NewCloner()

	err := c.gitSubmoduleSync(ctx, "/nonexistent/path")
	if err == nil {
		t.Error("gitSubmoduleSync() error = nil, want error for invalid directory")
	}
}

func TestGitSubmoduleUpdate_InvalidDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	c := NewCloner()

	err := c.gitSubmoduleUpdate(ctx, "/nonexistent/path")
	if err == nil {
		t.Error("gitSubmoduleUpdate() error = nil, want error for invalid directory")
	}
}

// Test helpers

func setupTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	createFile(t, filepath.Join(tmpDir, "README.md"), "# Test")
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "Initial commit")

	return tmpDir
}

func setupSubmodule(t *testing.T, parentRepo, subPath string) string {
	t.Helper()

	// Create the submodule repo
	subRepoDir := t.TempDir()
	runGit(t, subRepoDir, "init")
	runGit(t, subRepoDir, "config", "user.email", "test@test.com")
	runGit(t, subRepoDir, "config", "user.name", "Test")
	createFile(t, filepath.Join(subRepoDir, "sub.txt"), "submodule content")
	runGit(t, subRepoDir, "add", ".")
	runGit(t, subRepoDir, "commit", "-m", "Initial submodule commit")

	// Add as submodule to parent
	runGit(t, parentRepo, "submodule", "add", subRepoDir, subPath)
	runGit(t, parentRepo, "commit", "-m", "Add submodule")

	return filepath.Join(parentRepo, subPath)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func createFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
