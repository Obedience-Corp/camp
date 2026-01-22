package nav

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
)

func setupTestCampaign(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Resolve symlinks (macOS /var -> /private/var)
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("Failed to resolve symlinks: %v", err)
	}

	// Create .campaign directory
	campaignDir := filepath.Join(dir, ".campaign")
	if err := os.Mkdir(campaignDir, 0755); err != nil {
		t.Fatalf("Failed to create .campaign dir: %v", err)
	}

	// Create some category directories
	for _, cat := range []string{"projects", "festivals", "docs"} {
		if err := os.Mkdir(filepath.Join(dir, cat), 0755); err != nil {
			t.Fatalf("Failed to create %s dir: %v", cat, err)
		}
	}

	return dir
}

func TestDirectJump_CampaignRoot(t *testing.T) {
	dir := setupTestCampaign(t)

	// Change to campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Clear any cached detection
	campaign.ClearCache()

	ctx := context.Background()
	result, err := DirectJump(ctx, CategoryAll)
	if err != nil {
		t.Fatalf("DirectJump failed: %v", err)
	}

	if result.Path != dir {
		t.Errorf("Path = %q, want %q", result.Path, dir)
	}
	if !result.IsRoot {
		t.Error("IsRoot should be true")
	}
	if result.Category != CategoryAll {
		t.Errorf("Category = %q, want %q", result.Category, CategoryAll)
	}
}

func TestDirectJump_Categories(t *testing.T) {
	dir := setupTestCampaign(t)

	// Change to campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	tests := []struct {
		category Category
		wantDir  string
	}{
		{CategoryProjects, "projects"},
		{CategoryFestivals, "festivals"},
		{CategoryDocs, "docs"},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			result, err := DirectJump(ctx, tt.category)
			if err != nil {
				t.Fatalf("DirectJump failed: %v", err)
			}

			wantPath := filepath.Join(dir, tt.wantDir)
			if result.Path != wantPath {
				t.Errorf("Path = %q, want %q", result.Path, wantPath)
			}
			if result.IsRoot {
				t.Error("IsRoot should be false")
			}
			if result.Category != tt.category {
				t.Errorf("Category = %q, want %q", result.Category, tt.category)
			}
		})
	}
}

func TestDirectJump_MissingCategory(t *testing.T) {
	dir := setupTestCampaign(t)

	// Change to campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()
	// code_reviews doesn't exist
	_, err := DirectJump(ctx, CategoryCodeReviews)
	if err == nil {
		t.Fatal("Expected error for missing category")
	}

	var jumpErr *DirectJumpError
	if !errors.As(err, &jumpErr) {
		t.Fatalf("Expected DirectJumpError, got %T", err)
	}

	if !errors.Is(jumpErr.Err, ErrCategoryNotFound) {
		t.Errorf("Expected ErrCategoryNotFound, got %v", jumpErr.Err)
	}
	if jumpErr.Category != CategoryCodeReviews {
		t.Errorf("Category = %q, want %q", jumpErr.Category, CategoryCodeReviews)
	}
}

func TestDirectJump_NotACampaign(t *testing.T) {
	dir := t.TempDir()

	// Change to non-campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()
	_, err := DirectJump(ctx, CategoryProjects)
	if err == nil {
		t.Fatal("Expected error when not in campaign")
	}

	if !errors.Is(err, campaign.ErrNotInCampaign) {
		t.Errorf("Expected ErrNotInCampaign, got %v", err)
	}
}

func TestDirectJump_ContextCancellation(t *testing.T) {
	dir := setupTestCampaign(t)

	// Change to campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := DirectJump(ctx, CategoryProjects)
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
}

func TestDirectJumpFromRoot(t *testing.T) {
	dir := setupTestCampaign(t)

	ctx := context.Background()

	// Test campaign root
	result, err := DirectJumpFromRoot(ctx, dir, CategoryAll)
	if err != nil {
		t.Fatalf("DirectJumpFromRoot failed: %v", err)
	}
	if result.Path != dir {
		t.Errorf("Path = %q, want %q", result.Path, dir)
	}
	if !result.IsRoot {
		t.Error("IsRoot should be true")
	}

	// Test category
	result, err = DirectJumpFromRoot(ctx, dir, CategoryProjects)
	if err != nil {
		t.Fatalf("DirectJumpFromRoot failed: %v", err)
	}
	wantPath := filepath.Join(dir, "projects")
	if result.Path != wantPath {
		t.Errorf("Path = %q, want %q", result.Path, wantPath)
	}
}

func TestDirectJumpFromRoot_ContextCancellation(t *testing.T) {
	dir := setupTestCampaign(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := DirectJumpFromRoot(ctx, dir, CategoryProjects)
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestDirectJumpFromRoot_NotADirectory(t *testing.T) {
	dir := setupTestCampaign(t)

	// Create workflow directory first, then a file where pipelines directory is expected
	workflowDir := filepath.Join(dir, "workflow")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow dir: %v", err)
	}
	pipelines := filepath.Join(workflowDir, "pipelines")
	if err := os.WriteFile(pipelines, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	ctx := context.Background()
	_, err := DirectJumpFromRoot(ctx, dir, CategoryPipelines)
	if err == nil {
		t.Fatal("Expected error for file instead of directory")
	}

	var jumpErr *DirectJumpError
	if !errors.As(err, &jumpErr) {
		t.Fatalf("Expected DirectJumpError, got %T", err)
	}

	if !errors.Is(jumpErr.Err, ErrNotADirectory) {
		t.Errorf("Expected ErrNotADirectory, got %v", jumpErr.Err)
	}
}

func TestDirectJumpError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *DirectJumpError
		wantMsg string
	}{
		{
			name: "category not found",
			err: &DirectJumpError{
				Category: CategoryProjects,
				Path:     "/test/projects",
				Err:      ErrCategoryNotFound,
			},
			wantMsg: "category directory not found: projects (expected at /test/projects)",
		},
		{
			name: "not a directory",
			err: &DirectJumpError{
				Category: CategoryDocs,
				Path:     "/test/docs",
				Err:      ErrNotADirectory,
			},
			wantMsg: "category path is not a directory: /test/docs",
		},
		{
			name: "other error",
			err: &DirectJumpError{
				Category: CategoryFestivals,
				Path:     "/test/festivals",
				Err:      errors.New("permission denied"),
			},
			wantMsg: "failed to access category festivals: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if msg != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestDirectJumpError_Is(t *testing.T) {
	err1 := &DirectJumpError{
		Category: CategoryProjects,
		Path:     "/test/projects",
		Err:      ErrCategoryNotFound,
	}

	// Should match underlying error
	if !errors.Is(err1, ErrCategoryNotFound) {
		t.Error("Should match ErrCategoryNotFound")
	}

	// Should match same category and error
	err2 := &DirectJumpError{
		Category: CategoryProjects,
		Err:      ErrCategoryNotFound,
	}
	if !errors.Is(err1, err2) {
		t.Error("Should match similar DirectJumpError")
	}

	// Should not match different category
	err3 := &DirectJumpError{
		Category: CategoryDocs,
		Err:      ErrCategoryNotFound,
	}
	if errors.Is(err1, err3) {
		t.Error("Should not match different category")
	}
}

func TestDirectJumpError_Unwrap(t *testing.T) {
	underlying := errors.New("test error")
	err := &DirectJumpError{
		Category: CategoryProjects,
		Err:      underlying,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}
}

func BenchmarkDirectJump(b *testing.B) {
	dir := b.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Create campaign structure
	campaignDir := filepath.Join(dir, ".campaign")
	_ = os.Mkdir(campaignDir, 0755)
	_ = os.Mkdir(filepath.Join(dir, "projects"), 0755)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	_ = os.Chdir(dir)

	campaign.ClearCache()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DirectJump(ctx, CategoryProjects)
	}
}

func BenchmarkDirectJumpFromRoot(b *testing.B) {
	dir := b.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Create campaign structure
	campaignDir := filepath.Join(dir, ".campaign")
	_ = os.Mkdir(campaignDir, 0755)
	_ = os.Mkdir(filepath.Join(dir, "projects"), 0755)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DirectJumpFromRoot(ctx, dir, CategoryProjects)
	}
}

func TestDirectJump_Performance(t *testing.T) {
	// Verify direct jump meets the <50ms performance target
	dir := setupTestCampaign(t)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()
	start := time.Now()

	// Run multiple times to get average
	const iterations = 100
	for i := 0; i < iterations; i++ {
		_, err := DirectJump(ctx, CategoryProjects)
		if err != nil {
			t.Fatalf("DirectJump failed: %v", err)
		}
	}

	elapsed := time.Since(start)
	avgDuration := elapsed / iterations

	// Target is <50ms
	if avgDuration > 50*time.Millisecond {
		t.Errorf("DirectJump too slow: avg %v, want <50ms", avgDuration)
	}

	t.Logf("DirectJump average: %v", avgDuration)
}
