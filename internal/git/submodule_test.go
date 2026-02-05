package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsSubmodule_RegularRepo(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	os.MkdirAll(gitDir, 0755)

	result, err := IsSubmodule(tmpDir)
	if err != nil {
		t.Fatalf("IsSubmodule() error = %v", err)
	}
	if result {
		t.Error("IsSubmodule() = true, want false for regular repo")
	}
}

func TestIsSubmodule_Submodule(t *testing.T) {
	tmpDir := t.TempDir()
	gitFile := filepath.Join(tmpDir, ".git")
	os.WriteFile(gitFile, []byte("gitdir: ../.git/modules/sub"), 0644)

	result, err := IsSubmodule(tmpDir)
	if err != nil {
		t.Fatalf("IsSubmodule() error = %v", err)
	}
	if !result {
		t.Error("IsSubmodule() = false, want true for submodule")
	}
}

func TestIsSubmodule_NoGit(t *testing.T) {
	tmpDir := t.TempDir()
	// No .git at all

	result, err := IsSubmodule(tmpDir)
	if err != nil {
		t.Fatalf("IsSubmodule() error = %v", err)
	}
	if result {
		t.Error("IsSubmodule() = true, want false for no .git")
	}
}

func TestGetSubmoduleGitDir_RegularRepo(t *testing.T) {
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	os.MkdirAll(gitDir, 0755)

	result, err := GetSubmoduleGitDir(tmpDir)
	if err != nil {
		t.Fatalf("GetSubmoduleGitDir() error = %v", err)
	}
	if result != gitDir {
		t.Errorf("GetSubmoduleGitDir() = %v, want %v", result, gitDir)
	}
}

func TestGetSubmoduleGitDir_Submodule(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up submodule structure
	parentGit := filepath.Join(tmpDir, ".git", "modules", "child")
	os.MkdirAll(parentGit, 0755)

	childDir := filepath.Join(tmpDir, "child")
	os.MkdirAll(childDir, 0755)
	os.WriteFile(filepath.Join(childDir, ".git"), []byte("gitdir: ../.git/modules/child"), 0644)

	gitDir, err := GetSubmoduleGitDir(childDir)
	if err != nil {
		t.Fatalf("GetSubmoduleGitDir() error = %v", err)
	}

	if gitDir != parentGit {
		t.Errorf("GetSubmoduleGitDir() = %v, want %v", gitDir, parentGit)
	}
}

func TestFindProjectRoot(t *testing.T) {
	// Create repo structure
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")
	nested := filepath.Join(repoRoot, "a", "b", "c")
	os.MkdirAll(nested, 0755)
	os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755)

	// Find from nested path
	root, err := FindProjectRoot(nested)
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}
	if root != repoRoot {
		t.Errorf("FindProjectRoot() = %v, want %v", root, repoRoot)
	}
}

func TestFindProjectRoot_FromRoot(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	root, err := FindProjectRoot(tmpDir)
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}
	if root != tmpDir {
		t.Errorf("FindProjectRoot() = %v, want %v", root, tmpDir)
	}
}

func TestFindProjectRoot_NoRepo(t *testing.T) {
	tmpDir := t.TempDir()
	// No .git at all

	_, err := FindProjectRoot(tmpDir)
	if err == nil {
		t.Error("FindProjectRoot() expected error for no repo")
	}
}

func TestFindProjectRootWithType_RegularRepo(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	root, isSubmodule, err := FindProjectRootWithType(tmpDir)
	if err != nil {
		t.Fatalf("FindProjectRootWithType() error = %v", err)
	}
	if root != tmpDir {
		t.Errorf("root = %v, want %v", root, tmpDir)
	}
	if isSubmodule {
		t.Error("isSubmodule = true, want false")
	}
}

func TestFindProjectRootWithType_Submodule(t *testing.T) {
	tmpDir := t.TempDir()

	// Create parent git dir
	parentGitDir := filepath.Join(tmpDir, ".git", "modules", "sub")
	os.MkdirAll(parentGitDir, 0755)

	// Create submodule with gitdir file
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, ".git"), []byte("gitdir: ../.git/modules/sub"), 0644)

	root, isSubmodule, err := FindProjectRootWithType(subDir)
	if err != nil {
		t.Fatalf("FindProjectRootWithType() error = %v", err)
	}
	if root != subDir {
		t.Errorf("root = %v, want %v", root, subDir)
	}
	if !isSubmodule {
		t.Error("isSubmodule = false, want true")
	}
}

func TestFindParentRepository_NoParent(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	parent, err := FindParentRepository(tmpDir)
	if err != nil {
		t.Fatalf("FindParentRepository() error = %v", err)
	}
	if parent != "" {
		t.Errorf("FindParentRepository() = %v, want empty string", parent)
	}
}

func TestFindParentRepository_WithParent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create parent repo
	parentRepo := filepath.Join(tmpDir, "parent")
	os.MkdirAll(filepath.Join(parentRepo, ".git"), 0755)

	// Create child repo (submodule)
	childRepo := filepath.Join(parentRepo, "child")
	os.MkdirAll(filepath.Join(childRepo, ".git"), 0755)

	parent, err := FindParentRepository(childRepo)
	if err != nil {
		t.Fatalf("FindParentRepository() error = %v", err)
	}
	if parent != parentRepo {
		t.Errorf("FindParentRepository() = %v, want %v", parent, parentRepo)
	}
}

func TestGetSubmoduleInfo_Valid(t *testing.T) {
	tmpDir := t.TempDir()

	// Create parent git structure
	parentGitDir := filepath.Join(tmpDir, ".git", "modules", "sub")
	os.MkdirAll(parentGitDir, 0755)

	// Create parent .git directory (so parent can be found)
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	// Create submodule with gitdir file
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, ".git"), []byte("gitdir: ../.git/modules/sub"), 0644)

	info, err := GetSubmoduleInfo(subDir)
	if err != nil {
		t.Fatalf("GetSubmoduleInfo() error = %v", err)
	}

	if info.Path != subDir {
		t.Errorf("Path = %v, want %v", info.Path, subDir)
	}
	if info.GitDir != parentGitDir {
		t.Errorf("GitDir = %v, want %v", info.GitDir, parentGitDir)
	}
	if info.ParentRepo != tmpDir {
		t.Errorf("ParentRepo = %v, want %v", info.ParentRepo, tmpDir)
	}
}

func TestGetSubmoduleInfo_NotSubmodule(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	_, err := GetSubmoduleInfo(tmpDir)
	if err == nil {
		t.Error("GetSubmoduleInfo() expected error for non-submodule")
	}
}

func TestGetSubmoduleInfo_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create parent git structure
	parentGitDir := filepath.Join(tmpDir, ".git", "modules", "sub")
	os.MkdirAll(parentGitDir, 0755)

	// Create parent .git directory
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	// Create submodule with nested path
	subDir := filepath.Join(tmpDir, "sub")
	nestedDir := filepath.Join(subDir, "nested", "deep")
	os.MkdirAll(nestedDir, 0755)
	os.WriteFile(filepath.Join(subDir, ".git"), []byte("gitdir: ../.git/modules/sub"), 0644)

	info, err := GetSubmoduleInfo(nestedDir)
	if err != nil {
		t.Fatalf("GetSubmoduleInfo() error = %v", err)
	}

	if info.Path != subDir {
		t.Errorf("Path = %v, want %v", info.Path, subDir)
	}
}

// setupSubmoduleConfig adds a submodule entry to .gitmodules and optionally .git/config.
func setupSubmoduleConfig(t *testing.T, repoRoot, submodulePath, declaredURL, activeURL string) {
	t.Helper()

	// Add to .gitmodules
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
		"submodule."+submodulePath+".url", declaredURL)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set .gitmodules URL: %v", err)
	}

	cmd = exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules",
		"submodule."+submodulePath+".path", submodulePath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set .gitmodules path: %v", err)
	}

	// Add to .git/config if activeURL is provided
	if activeURL != "" {
		cmd = exec.Command("git", "-C", repoRoot, "config",
			"submodule."+submodulePath+".url", activeURL)
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to set .git/config URL: %v", err)
		}
	}
}

func TestGetDeclaredURL(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	expectedURL := "git@github.com:org/test-sub.git"

	setupSubmoduleConfig(t, repoRoot, submodulePath, expectedURL, "")

	url, err := GetDeclaredURL(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("GetDeclaredURL() error = %v", err)
	}
	if url != expectedURL {
		t.Errorf("GetDeclaredURL() = %q, want %q", url, expectedURL)
	}
}

func TestGetDeclaredURL_NotFound(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)

	_, err := GetDeclaredURL(ctx, repoRoot, "nonexistent")
	if err == nil {
		t.Error("GetDeclaredURL() expected error for nonexistent submodule")
	}
}

func TestGetDeclaredURL_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupGitRepo(t)

	_, err := GetDeclaredURL(ctx, repoRoot, "any")
	if err != context.Canceled {
		t.Errorf("GetDeclaredURL() error = %v, want context.Canceled", err)
	}
}

func TestGetActiveURL(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	declaredURL := "git@github.com:org/test-sub.git"
	activeURL := "git@github.com:other/test-sub.git"

	setupSubmoduleConfig(t, repoRoot, submodulePath, declaredURL, activeURL)

	url, err := GetActiveURL(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("GetActiveURL() error = %v", err)
	}
	if url != activeURL {
		t.Errorf("GetActiveURL() = %q, want %q", url, activeURL)
	}
}

func TestGetActiveURL_NotInitialized(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"

	// Only set .gitmodules, not .git/config (simulates uninitialized submodule)
	setupSubmoduleConfig(t, repoRoot, submodulePath, "git@github.com:org/repo.git", "")

	url, err := GetActiveURL(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("GetActiveURL() error = %v", err)
	}
	if url != "" {
		t.Errorf("GetActiveURL() = %q, want empty string for uninitialized", url)
	}
}

func TestGetActiveURL_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupGitRepo(t)

	_, err := GetActiveURL(ctx, repoRoot, "any")
	if err != context.Canceled {
		t.Errorf("GetActiveURL() error = %v, want context.Canceled", err)
	}
}

func TestCompareURLs_Match(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	url := "git@github.com:org/test-sub.git"

	setupSubmoduleConfig(t, repoRoot, submodulePath, url, url)

	result, err := CompareURLs(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("CompareURLs() error = %v", err)
	}
	if !result.Match {
		t.Errorf("CompareURLs().Match = false, want true")
	}
	if result.DeclaredURL != url {
		t.Errorf("DeclaredURL = %q, want %q", result.DeclaredURL, url)
	}
	if result.ActiveURL != url {
		t.Errorf("ActiveURL = %q, want %q", result.ActiveURL, url)
	}
}

func TestCompareURLs_Mismatch(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	declaredURL := "git@github.com:new-org/test-sub.git"
	activeURL := "git@github.com:old-org/test-sub.git"

	setupSubmoduleConfig(t, repoRoot, submodulePath, declaredURL, activeURL)

	result, err := CompareURLs(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("CompareURLs() error = %v", err)
	}
	if result.Match {
		t.Errorf("CompareURLs().Match = true, want false for different URLs")
	}
	if result.DeclaredURL != declaredURL {
		t.Errorf("DeclaredURL = %q, want %q", result.DeclaredURL, declaredURL)
	}
	if result.ActiveURL != activeURL {
		t.Errorf("ActiveURL = %q, want %q", result.ActiveURL, activeURL)
	}
}

func TestCompareURLs_TrailingSlashNormalization(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	declaredURL := "https://github.com/org/repo/"
	activeURL := "https://github.com/org/repo"

	setupSubmoduleConfig(t, repoRoot, submodulePath, declaredURL, activeURL)

	result, err := CompareURLs(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("CompareURLs() error = %v", err)
	}
	if !result.Match {
		t.Errorf("CompareURLs().Match = false, want true (trailing slash should be normalized)")
	}
}

func TestCompareURLs_UninitializedSubmodule(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupGitRepo(t)
	submodulePath := "projects/test-sub"
	declaredURL := "git@github.com:org/test-sub.git"

	// Only set .gitmodules
	setupSubmoduleConfig(t, repoRoot, submodulePath, declaredURL, "")

	result, err := CompareURLs(ctx, repoRoot, submodulePath)
	if err != nil {
		t.Fatalf("CompareURLs() error = %v", err)
	}
	if result.Match {
		t.Errorf("CompareURLs().Match = true, want false for uninitialized submodule")
	}
	if result.DeclaredURL != declaredURL {
		t.Errorf("DeclaredURL = %q, want %q", result.DeclaredURL, declaredURL)
	}
	if result.ActiveURL != "" {
		t.Errorf("ActiveURL = %q, want empty string", result.ActiveURL)
	}
}

func TestCompareURLs_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupGitRepo(t)

	_, err := CompareURLs(ctx, repoRoot, "any")
	if err != context.Canceled {
		t.Errorf("CompareURLs() error = %v, want context.Canceled", err)
	}
}

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no trailing slash", "git@github.com:org/repo.git", "git@github.com:org/repo.git"},
		{"trailing slash", "https://github.com/org/repo/", "https://github.com/org/repo"},
		{"with whitespace", "  git@github.com:org/repo.git  ", "git@github.com:org/repo.git"},
		{"trailing slash and whitespace", "  https://github.com/org/repo/  ", "https://github.com/org/repo"},
		{"empty string", "", ""},
		{"relative path", "../repo.git", "../repo.git"},
		{"relative with trailing slash", "../repo/", "../repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeGitURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeGitURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
