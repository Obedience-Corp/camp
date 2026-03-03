package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePath(t *testing.T) {
	path := CachePath("/test/campaign")
	expected := "/test/campaign/.campaign/cache/nav-index.json"
	if path != expected {
		t.Errorf("CachePath() = %q, want %q", path, expected)
	}
}

func TestSave(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	idx := NewIndex(root)
	idx.AddTarget(Target{Name: "test", Path: "/test/path", Category: "projects"})

	err := Save(idx, root)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	path := CachePath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Cache file was not created")
	}

	// Verify content is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}
	if len(data) == 0 {
		t.Error("Cache file is empty")
	}
}

func TestSave_NilIndex(t *testing.T) {
	root := t.TempDir()
	err := Save(nil, root)
	if err == nil {
		t.Error("Save(nil) should return error")
	}
}

func TestSave_CreatesCacheDir(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	idx := NewIndex(root)

	// Cache directory doesn't exist yet
	cacheDir := filepath.Join(root, ".campaign", "cache")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("Cache dir should not exist yet")
	}

	err := Save(idx, root)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Now it should exist
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Error("Save should have created cache directory")
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create and save an index
	original := NewIndex(root)
	original.AddTarget(Target{Name: "test-project", Path: "/test/projects/test-project", Category: "projects"})

	if err := Save(original, root); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load it back
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded == nil {
		t.Fatal("Load() returned nil")
	}

	if len(loaded.Targets) != 1 {
		t.Errorf("Loaded %d targets, want 1", len(loaded.Targets))
	}

	if loaded.Targets[0].Name != "test-project" {
		t.Errorf("Loaded target name = %q, want %q", loaded.Targets[0].Name, "test-project")
	}
}

func TestLoad_NoCache(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	idx, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if idx != nil {
		t.Error("Load() should return nil for non-existent cache")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create invalid cache file
	cacheDir := filepath.Join(root, ".campaign", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, "nav-index.json")
	if err := os.WriteFile(cachePath, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)
	if err == nil {
		t.Error("Load() should error on invalid JSON")
	}
}

func TestIsFresh(t *testing.T) {
	tests := []struct {
		name      string
		buildTime time.Time
		want      bool
	}{
		{
			name:      "fresh cache",
			buildTime: time.Now(),
			want:      true,
		},
		{
			name:      "slightly old",
			buildTime: time.Now().Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "old cache",
			buildTime: time.Now().Add(-25 * time.Hour),
			want:      false,
		},
		{
			name:      "very old cache",
			buildTime: time.Now().Add(-7 * 24 * time.Hour),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := &Index{BuildTime: tt.buildTime}
			if got := IsFresh(idx); got != tt.want {
				t.Errorf("IsFresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFresh_NilIndex(t *testing.T) {
	if IsFresh(nil) {
		t.Error("IsFresh(nil) should return false")
	}
}

func TestIsStale(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Fresh index with current version, no trigger files
	idx := &Index{BuildTime: time.Now(), Version: IndexVersion}
	if IsStale(idx, root) {
		t.Error("Fresh index should not be stale")
	}
}

func TestIsStale_VersionMismatch(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Fresh index but with old version
	idx := &Index{BuildTime: time.Now(), Version: IndexVersion - 1}
	if !IsStale(idx, root) {
		t.Error("Index with old version should be stale")
	}
}

func TestIsStale_BinaryRebuilt(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Index built in the past, binary is newer
	idx := &Index{
		BuildTime: time.Now().Add(-1 * time.Hour),
		Version:   IndexVersion,
	}

	// The running binary's mod time is likely after an hour ago,
	// so this should detect staleness from the binary check.
	campBin, err := os.Executable()
	if err != nil {
		t.Skip("cannot determine executable path")
	}
	info, err := os.Stat(campBin)
	if err != nil {
		t.Skip("cannot stat executable")
	}

	// Only assert stale if the binary is actually newer than build time
	if info.ModTime().After(idx.BuildTime) {
		if !IsStale(idx, root) {
			t.Error("Index should be stale when binary is newer")
		}
	}
}

func TestIsStale_NilIndex(t *testing.T) {
	root := t.TempDir()
	if !IsStale(nil, root) {
		t.Error("nil index should be stale")
	}
}

func TestIsStale_OldIndex(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	idx := &Index{BuildTime: time.Now().Add(-25 * time.Hour)}
	if !IsStale(idx, root) {
		t.Error("Old index should be stale")
	}
}

func TestIsStale_GitmodulesChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create .gitmodules file
	gitmodules := filepath.Join(root, ".gitmodules")
	if err := os.WriteFile(gitmodules, []byte("[submodule]"), 0644); err != nil {
		t.Fatal(err)
	}

	// Index is older than .gitmodules
	idx := &Index{BuildTime: time.Now().Add(-1 * time.Hour)}

	// Update .gitmodules modification time to now
	now := time.Now()
	if err := os.Chtimes(gitmodules, now, now); err != nil {
		t.Fatal(err)
	}

	if !IsStale(idx, root) {
		t.Error("Index should be stale when .gitmodules is newer")
	}
}

func TestIsStale_CampaignYamlChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create campaign.yaml
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatal(err)
	}
	campaignYaml := filepath.Join(campaignDir, "campaign.yaml")
	if err := os.WriteFile(campaignYaml, []byte("name: test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Index is older than campaign.yaml
	idx := &Index{BuildTime: time.Now().Add(-1 * time.Hour)}

	// Update campaign.yaml modification time to now
	now := time.Now()
	if err := os.Chtimes(campaignYaml, now, now); err != nil {
		t.Fatal(err)
	}

	if !IsStale(idx, root) {
		t.Error("Index should be stale when campaign.yaml is newer")
	}
}

func TestGetOrBuild(t *testing.T) {
	root := setupTestCampaign(t)

	ctx := context.Background()
	idx, err := GetOrBuild(ctx, root, false)
	if err != nil {
		t.Fatalf("GetOrBuild() error = %v", err)
	}

	if idx == nil {
		t.Fatal("GetOrBuild() returned nil")
	}

	// Cache should be saved
	path := CachePath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("GetOrBuild should save cache")
	}
}

func TestGetOrBuild_UsesCache(t *testing.T) {
	root := setupTestCampaign(t)

	ctx := context.Background()

	// First call builds
	idx1, _ := GetOrBuild(ctx, root, false)

	// Second call should use cache
	idx2, err := GetOrBuild(ctx, root, false)
	if err != nil {
		t.Fatalf("GetOrBuild() error = %v", err)
	}

	// Both should have same build time (cached)
	if !idx1.BuildTime.Equal(idx2.BuildTime) {
		t.Error("Second call should have used cache (same build time)")
	}
}

func TestGetOrBuild_ForceRebuild(t *testing.T) {
	root := setupTestCampaign(t)

	ctx := context.Background()

	// First call builds
	idx1, _ := GetOrBuild(ctx, root, false)

	// Small delay to ensure different build time
	time.Sleep(10 * time.Millisecond)

	// Force rebuild should create new index
	idx2, err := GetOrBuild(ctx, root, true)
	if err != nil {
		t.Fatalf("GetOrBuild() error = %v", err)
	}

	// Build times should be different
	if idx1.BuildTime.Equal(idx2.BuildTime) {
		t.Error("Force rebuild should create new index with different build time")
	}
}

func TestGetOrBuild_ContextCancellation(t *testing.T) {
	root := setupTestCampaign(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GetOrBuild(ctx, root, false)
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestDelete(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create cache
	idx := NewIndex(root)
	if err := Save(idx, root); err != nil {
		t.Fatal(err)
	}

	// Verify it exists
	path := CachePath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Cache should exist")
	}

	// Delete it
	if err := Delete(root); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Cache should be deleted")
	}
}

func TestDelete_NoCache(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Should not error if cache doesn't exist
	if err := Delete(root); err != nil {
		t.Errorf("Delete() should not error for non-existent cache: %v", err)
	}
}

func TestInfo(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Before cache exists
	info, err := Info(root)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.Exists {
		t.Error("Info should show cache doesn't exist")
	}
	if info.Path != CachePath(root) {
		t.Errorf("Info.Path = %q, want %q", info.Path, CachePath(root))
	}

	// Create cache
	idx := NewIndex(root)
	if err := Save(idx, root); err != nil {
		t.Fatal(err)
	}

	// After cache exists
	info, err = Info(root)
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if !info.Exists {
		t.Error("Info should show cache exists")
	}
	if info.Size == 0 {
		t.Error("Info.Size should be > 0")
	}
	if info.ModTime.IsZero() {
		t.Error("Info.ModTime should not be zero")
	}
	if !info.Fresh {
		t.Error("Newly created cache should be fresh")
	}
}

func TestIsStale_ProjectsDirChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create projects/ directory
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Index built an hour ago
	idx := &Index{BuildTime: time.Now().Add(-1 * time.Hour), Version: IndexVersion}

	// Set projects/ dir mtime to now (simulating a directory add/remove/rename)
	now := time.Now()
	if err := os.Chtimes(projectsDir, now, now); err != nil {
		t.Fatal(err)
	}

	if !IsStale(idx, root) {
		t.Error("Index should be stale when projects/ directory is newer")
	}
}

func TestIsStale_ProjectsDirDoesNotExist(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Fresh index, no projects/ directory
	idx := &Index{BuildTime: time.Now(), Version: IndexVersion}

	if IsStale(idx, root) {
		t.Error("Index should not be stale when projects/ directory does not exist")
	}
}

func TestIsStale_WorktreesDirChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	worktreesDir := filepath.Join(root, "projects", "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}

	idx := &Index{
		BuildTime: time.Now().Add(-1 * time.Hour),
		Version:   IndexVersion,
	}

	now := time.Now()
	if err := os.Chtimes(worktreesDir, now, now); err != nil {
		t.Fatal(err)
	}

	if !IsStale(idx, root) {
		t.Error("Index should be stale when projects/worktrees/ directory is newer")
	}
}

func TestIsStale_WorktreeProjectDirChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	worktreesDir := filepath.Join(root, "projects", "worktrees")
	projectDir := filepath.Join(worktreesDir, "camp")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	buildTime := time.Now().Add(-1 * time.Hour)
	idx := &Index{
		BuildTime: buildTime,
		Version:   IndexVersion,
	}

	// Keep projects/worktrees older than the index build time so this test
	// specifically validates per-project worktree directory invalidation.
	older := buildTime.Add(-1 * time.Minute)
	if err := os.Chtimes(worktreesDir, older, older); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := os.Chtimes(projectDir, now, now); err != nil {
		t.Fatal(err)
	}

	if !IsStale(idx, root) {
		t.Error("Index should be stale when projects/worktrees/<project>/ is newer")
	}
}

// Benchmarks

func BenchmarkLoad(b *testing.B) {
	root := b.TempDir()

	// Create a cache with some targets
	idx := NewIndex(root)
	for i := 0; i < 100; i++ {
		idx.AddTarget(Target{
			Name:     "target-" + string(rune('a'+i%26)),
			Path:     "/test/path/" + string(rune('a'+i%26)),
			Category: "projects",
		})
	}
	if err := Save(idx, root); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Load(root)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSave(b *testing.B) {
	root := b.TempDir()

	idx := NewIndex(root)
	for i := 0; i < 100; i++ {
		idx.AddTarget(Target{
			Name:     "target-" + string(rune('a'+i%26)),
			Path:     "/test/path/" + string(rune('a'+i%26)),
			Category: "projects",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Save(idx, root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsFresh(b *testing.B) {
	idx := &Index{BuildTime: time.Now()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsFresh(idx)
	}
}

func BenchmarkIsStale(b *testing.B) {
	root := b.TempDir()
	idx := &Index{BuildTime: time.Now()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsStale(idx, root)
	}
}
