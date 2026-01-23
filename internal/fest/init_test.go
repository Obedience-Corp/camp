package fest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunInit(t *testing.T) {
	ResetCache()
	if !IsFestAvailable() {
		t.Skip("fest not installed")
	}

	tmpDir := t.TempDir()
	ctx := context.Background()

	err := RunInit(ctx, &InitOptions{
		CampaignRoot: tmpDir,
	})
	if err != nil {
		t.Fatalf("RunInit() error = %v", err)
	}

	// Verify initialization
	if !IsInitialized(tmpDir) {
		t.Error("festivals not initialized after RunInit")
	}
}

func TestRunInit_AlreadyInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	festivalsDir := filepath.Join(tmpDir, "festivals", ".festival")
	if err := os.MkdirAll(festivalsDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	ctx := context.Background()
	err := RunInit(ctx, &InitOptions{CampaignRoot: tmpDir})

	// Should succeed without running fest (already initialized)
	if err != nil {
		t.Errorf("RunInit() error = %v for already initialized", err)
	}
}

func TestIsInitialized(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  bool
	}{
		{
			name: "not initialized",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "festivals"), 0755)
			},
			want: false,
		},
		{
			name: "has .festival directory",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "festivals", ".festival"), 0755)
			},
			want: true,
		},
		{
			name: "has fest.yaml",
			setup: func(dir string) {
				festDir := filepath.Join(dir, "festivals")
				os.MkdirAll(festDir, 0755)
				os.WriteFile(filepath.Join(festDir, "fest.yaml"), []byte("test"), 0644)
			},
			want: true,
		},
		{
			name: "has .fest",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "festivals", ".fest"), 0755)
			},
			want: true,
		},
		{
			name: "no festivals directory",
			setup: func(dir string) {
				// Don't create anything
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(tmpDir)

			got := IsInitialized(tmpDir)
			if got != tt.want {
				t.Errorf("IsInitialized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunInit_FestNotAvailable(t *testing.T) {
	// This test verifies the error path when fest is not found.
	// Skip if fest is available via any method (including fallback paths)
	ResetCache()
	if IsFestAvailable() {
		t.Skip("fest is available, cannot test not-found path")
	}

	tmpDir := t.TempDir()
	ctx := context.Background()

	err := RunInit(ctx, &InitOptions{
		CampaignRoot: tmpDir,
	})

	// Should return ErrFestNotFound
	if err == nil {
		t.Error("RunInit() should fail when fest is not found")
	}
}
