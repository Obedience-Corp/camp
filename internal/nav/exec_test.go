package nav

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestExecInDir_Success(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	ctx := context.Background()

	// Run 'echo hello' command
	result, err := ExecInDir(ctx, dir, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("ExecInDir failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Dir != dir {
		t.Errorf("Dir = %q, want %q", result.Dir, dir)
	}
}

func TestExecInDir_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	ctx := context.Background()

	// Run 'false' command which exits with 1
	result, err := ExecInDir(ctx, dir, []string{"false"})
	if err != nil {
		t.Fatalf("ExecInDir failed: %v", err)
	}

	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestExecInDir_CommandNotFound(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	ctx := context.Background()

	// Run non-existent command
	_, err := ExecInDir(ctx, dir, []string{"nonexistent-command-12345"})
	if err == nil {
		t.Fatal("Expected error for non-existent command")
	}

	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Expected CommandError, got %T", err)
	}

	if cmdErr.Command != "nonexistent-command-12345" {
		t.Errorf("Command = %q, want %q", cmdErr.Command, "nonexistent-command-12345")
	}
}

func TestExecInDir_NoCommand(t *testing.T) {
	dir := t.TempDir()

	ctx := context.Background()

	_, err := ExecInDir(ctx, dir, nil)
	if err == nil {
		t.Fatal("Expected error for no command")
	}

	if !errors.Is(err, ErrNoCommand) {
		t.Errorf("Expected ErrNoCommand, got %v", err)
	}

	// Also test empty slice
	_, err = ExecInDir(ctx, dir, []string{})
	if !errors.Is(err, ErrNoCommand) {
		t.Errorf("Expected ErrNoCommand for empty slice, got %v", err)
	}
}

func TestExecInDir_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ExecInDir(ctx, dir, []string{"echo", "hello"})
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestExecInDir_WorksFromDir(t *testing.T) {
	// Create a temp directory with a file
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	testFile := filepath.Join(dir, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Use ls to verify we're in the right directory
	// (the file should exist)
	result, err := ExecInDir(ctx, dir, []string{"ls", "testfile.txt"})
	if err != nil {
		t.Fatalf("ExecInDir failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (file should exist)", result.ExitCode)
	}
}

func TestExecInCategory_Success(t *testing.T) {
	dir := setupTestCampaign(t)

	// Change to campaign directory
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()

	// Run 'pwd' in projects directory
	result, err := ExecInCategory(ctx, CategoryProjects, []string{"pwd"})
	if err != nil {
		t.Fatalf("ExecInCategory failed: %v", err)
	}

	expectedDir := filepath.Join(dir, "projects")
	if result.Dir != expectedDir {
		t.Errorf("Dir = %q, want %q", result.Dir, expectedDir)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestExecInCategory_NoCommand(t *testing.T) {
	dir := setupTestCampaign(t)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()

	_, err := ExecInCategory(ctx, CategoryProjects, nil)
	if !errors.Is(err, ErrNoCommand) {
		t.Errorf("Expected ErrNoCommand, got %v", err)
	}
}

func TestExecInCategory_MissingCategory(t *testing.T) {
	dir := setupTestCampaign(t)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()

	// code_reviews doesn't exist in our test setup
	_, err := ExecInCategory(ctx, CategoryCodeReviews, []string{"ls"})
	if err == nil {
		t.Fatal("Expected error for missing category")
	}

	var jumpErr *DirectJumpError
	if !errors.As(err, &jumpErr) {
		t.Fatalf("Expected DirectJumpError, got %T", err)
	}
}

func TestExecInCategory_CampaignRoot(t *testing.T) {
	dir := setupTestCampaign(t)

	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	campaign.ClearCache()

	ctx := context.Background()

	// Run command from campaign root
	result, err := ExecInCategory(ctx, CategoryAll, []string{"pwd"})
	if err != nil {
		t.Fatalf("ExecInCategory failed: %v", err)
	}

	if result.Dir != dir {
		t.Errorf("Dir = %q, want %q", result.Dir, dir)
	}
}

func BenchmarkExecInDir(b *testing.B) {
	dir := b.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExecInDir(ctx, dir, []string{"true"})
	}
}
