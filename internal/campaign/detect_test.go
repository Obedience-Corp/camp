package campaign

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	// Resolve symlinks for comparison (macOS /var -> /private/var)
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "my-campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)
	nestedDir := filepath.Join(campaignRoot, "projects", "foo", "bar")

	// Create directories
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name     string
		startDir string
		want     string
		wantErr  error
		setupEnv string
		cleanEnv bool
	}{
		{
			name:     "detect from campaign root",
			startDir: campaignRoot,
			want:     campaignRoot,
		},
		{
			name:     "detect from nested directory",
			startDir: nestedDir,
			want:     campaignRoot,
		},
		{
			name:     "detect from immediate child",
			startDir: filepath.Join(campaignRoot, "projects"),
			want:     campaignRoot,
		},
		{
			name:     "not in campaign",
			startDir: tmpDir, // parent of campaign root
			wantErr:  ErrNotInCampaign,
		},
		{
			name:     "env var override",
			startDir: tmpDir, // would fail without env var
			setupEnv: campaignRoot,
			want:     campaignRoot,
			cleanEnv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv != "" {
				os.Setenv(EnvCampaignRoot, tt.setupEnv)
			}
			if tt.cleanEnv {
				defer os.Unsetenv(EnvCampaignRoot)
			}

			got, err := Detect(ctx, tt.startDir)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Detect() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Detect() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetect_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Detect(ctx, "/some/path")
	if err != context.Canceled {
		t.Errorf("Detect() with cancelled context: got %v, want %v", err, context.Canceled)
	}
}

func TestDetect_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // Ensure timeout

	_, err := Detect(ctx, "/some/path")
	if err != context.DeadlineExceeded {
		t.Errorf("Detect() with timed out context: got %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestDetectFromCwd(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	// Resolve symlinks for comparison (macOS /var -> /private/var)
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "test-campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)

	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Change to campaign root
	if err := os.Chdir(campaignRoot); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	ctx := context.Background()
	got, err := DetectFromCwd(ctx)
	if err != nil {
		t.Errorf("DetectFromCwd() error = %v", err)
		return
	}

	if got != campaignRoot {
		t.Errorf("DetectFromCwd() = %v, want %v", got, campaignRoot)
	}
}

func TestIsCampaignRoot(t *testing.T) {
	tmpDir := t.TempDir()
	campaignRoot := filepath.Join(tmpDir, "campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)

	// Not a campaign yet
	if err := os.MkdirAll(campaignRoot, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if IsCampaignRoot(campaignRoot) {
		t.Error("IsCampaignRoot() returned true for non-campaign")
	}

	// Create campaign marker
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}
	if !IsCampaignRoot(campaignRoot) {
		t.Error("IsCampaignRoot() returned false for valid campaign")
	}
}

func TestCampaignPath(t *testing.T) {
	got := CampaignPath("/foo/bar")
	want := filepath.Join("/foo/bar", CampaignDir)
	if got != want {
		t.Errorf("CampaignPath() = %v, want %v", got, want)
	}
}

func BenchmarkDetect(b *testing.B) {
	tmpDir := b.TempDir()
	campaignRoot := filepath.Join(tmpDir, "campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)
	deepDir := filepath.Join(campaignRoot, "a", "b", "c", "d", "e", "f", "g", "h")

	os.MkdirAll(campaignDir, 0755)
	os.MkdirAll(deepDir, 0755)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Detect(ctx, deepDir)
	}
}
