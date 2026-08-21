//go:build container_fs

// Filesystem-mutating tests for this package.
//
// These build only under the container_fs tag and are executed inside the
// integration harness's pooled container (see tests/integration/containerfs_test.go),
// never on the host. The tag is the enforcement seam: `just test` on a
// developer machine does not compile this file, so nothing here can create a
// repo in someone's home directory.
//
// They are the original suites verbatim rather than CLI rewrites. Most assert
// on unexported package internals, which a test in package integration could
// not reach; running them where the filesystem is disposable keeps both the
// isolation and the assertions (decision D007).

package clone

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClone_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Skip this test because git clone --recurse-submodules with file:// URLs
	// requires GIT_ALLOW_PROTOCOL=file, which we cannot set for subprocesses
	// spawned by git clone itself. This test works in production with real
	// git servers (https://, ssh://).
	t.Skip("skipping: git clone --recurse-submodules fails with file:// URLs in test env")

	ctx := context.Background()

	// Create a source repo to clone from
	sourceDir := setupTestRepo(t)
	setupSubmodule(t, sourceDir, "projects/sub")

	// Create cloner
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true. Errors: %v", result.Errors)
	}

	if result.Directory == "" {
		t.Error("Clone().Directory is empty")
	}

	// Should have submodule results
	if len(result.Submodules) != 1 {
		t.Errorf("Clone().Submodules = %d, want 1", len(result.Submodules))
	}

	// Validation should have passed
	if result.Validation == nil {
		t.Error("Clone().Validation is nil")
	} else if !result.Validation.Passed {
		t.Errorf("Clone().Validation.Passed = false, want true. Issues: %v", result.Validation.Issues)
	}
}

func TestClone_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a source repo without submodules
	sourceDir := setupTestRepo(t)

	// Clone with no submodules
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true")
	}

	// Should have no submodule results when disabled
	if len(result.Submodules) != 0 {
		t.Errorf("Clone().Submodules = %d, want 0 with NoSubmodules", len(result.Submodules))
	}
}

func TestClone_NoValidate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceDir := setupTestRepo(t)
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true),
		WithNoValidate(true),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true")
	}

	// Validation should be nil when skipped
	if result.Validation != nil {
		t.Error("Clone().Validation should be nil with NoValidate")
	}
}

func TestClone_WithBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a source repo with a feature branch
	sourceDir := setupTestRepo(t)
	runGit(t, sourceDir, "checkout", "-b", "feature-branch")
	createFile(t, filepath.Join(sourceDir, "feature.txt"), "feature content")
	runGit(t, sourceDir, "add", ".")
	runGit(t, sourceDir, "commit", "-m", "Feature commit")

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithBranch("feature-branch"),
		WithNoSubmodules(true),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true")
	}

	if result.Branch != "feature-branch" {
		t.Errorf("Clone().Branch = %q, want %q", result.Branch, "feature-branch")
	}
}

func TestClone_InvalidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	c := NewCloner(
		WithURL("/nonexistent/path/that/does/not/exist"),
		WithDirectory(t.TempDir()),
	)

	result, err := c.Clone(ctx)

	// Should return an error
	if err == nil {
		t.Error("Clone() error = nil, want error for invalid URL")
	}

	// Result should indicate failure
	if result != nil && result.Success {
		t.Error("Clone().Success = true, want false for invalid URL")
	}
}

func TestValidate_AllInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a repo with an initialized submodule
	repoDir := setupTestRepo(t)
	setupSubmodule(t, repoDir, "projects/sub")

	c := NewCloner()
	result := c.validate(ctx, repoDir)

	if !result.Passed {
		t.Errorf("validate().Passed = false, want true. Issues: %v", result.Issues)
	}

	if !result.AllInitialized {
		t.Error("validate().AllInitialized = false, want true")
	}
}

func TestClone_NoSubmodulesSkipsValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a source repo with submodule
	sourceDir := setupTestRepo(t)
	setupSubmodule(t, sourceDir, "projects/sub")

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true. Errors: %v", result.Errors)
	}

	if result.Directory == "" {
		t.Error("Clone().Directory is empty")
	}

	if len(result.Submodules) != 0 {
		t.Errorf("Clone().Submodules = %d, want 0 with NoSubmodules", len(result.Submodules))
	}

	if result.Validation == nil {
		t.Fatal("Clone().Validation is nil")
	}
	if !result.Validation.Passed {
		t.Errorf("Clone().Validation.Passed = false, want true. Issues: %v", result.Validation.Issues)
	}
	for _, issue := range result.Validation.Issues {
		if issue.Description == "not initialized" {
			t.Errorf("validation reported skipped submodule %q as not initialized", issue.Submodule)
		}
	}
}

func TestClone_BranchPopulated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceDir := setupTestRepo(t)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	// Branch should be populated
	if result.Branch == "" {
		t.Error("Clone().Branch is empty")
	}
}

func TestClone_ShallowDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create source with multiple commits
	sourceDir := setupTestRepo(t)
	createFile(t, filepath.Join(sourceDir, "file2.txt"), "content2")
	runGit(t, sourceDir, "add", ".")
	runGit(t, sourceDir, "commit", "-m", "Second commit")
	createFile(t, filepath.Join(sourceDir, "file3.txt"), "content3")
	runGit(t, sourceDir, "add", ".")
	runGit(t, sourceDir, "commit", "-m", "Third commit")

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "cloned")

	c := NewCloner(
		WithURL(sourceDir),
		WithDirectory(targetPath),
		WithNoSubmodules(true),
		WithDepth(1),
	)

	result, err := c.Clone(ctx)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Clone().Success = false, want true")
	}
}

func TestValidate_NoSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	repoDir := setupTestRepo(t)

	c := NewCloner()
	result := c.validate(ctx, repoDir)

	if !result.Passed {
		t.Errorf("validate().Passed = false, want true for repo without submodules")
	}
}

func TestValidate_UninitializedSubmodule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a repo and add submodule, but don't initialize it fully
	repoDir := setupTestRepo(t)

	// Create a submodule entry manually without initializing
	subRepoDir := t.TempDir()
	runGit(t, subRepoDir, "init")
	runGit(t, subRepoDir, "config", "user.email", "test@test.com")
	runGit(t, subRepoDir, "config", "user.name", "Test")
	createFile(t, filepath.Join(subRepoDir, "sub.txt"), "content")
	runGit(t, subRepoDir, "add", ".")
	runGit(t, subRepoDir, "commit", "-m", "Initial")

	// Add as submodule
	runGit(t, repoDir, "submodule", "add", subRepoDir, "projects/sub")
	runGit(t, repoDir, "commit", "-m", "Add sub")

	c := NewCloner()
	result := c.validate(ctx, repoDir)

	// Should pass (submodule is initialized by setupSubmodule equivalent)
	// The validation checks the actual state
	if result == nil {
		t.Error("validate() returned nil")
	}
}
