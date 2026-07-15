package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/nav"
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

func TestSave_OmitsTargetLastAccess(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	idx := NewIndex(root)
	idx.AddTarget(Target{Name: "test", Path: "/test/path", Category: "projects"})

	if err := Save(idx, root); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(CachePath(root))
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	if strings.Contains(string(data), "\"last_access\"") {
		t.Fatalf("cache file should not persist target last_access: %s", data)
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

func TestIsStale_WorkflowTopologyChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	buildTime := time.Now()
	workflowDir := filepath.Join(root, nav.CategoryWorkflow.Dir())
	designDir := filepath.Join(workflowDir, "design")
	if err := os.MkdirAll(designDir, 0755); err != nil {
		t.Fatal(err)
	}

	older := buildTime.Add(-1 * time.Minute)
	if err := os.Chtimes(workflowDir, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(designDir, buildTime.Add(1*time.Minute), buildTime.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	idx := &Index{BuildTime: buildTime, Version: IndexVersion}
	if !IsStale(idx, root) {
		t.Error("Index should be stale when workflow immediate-child topology is newer")
	}
}

func TestIsStale_IntentsTopologyChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	buildTime := time.Now()
	intentsDir := filepath.Join(root, nav.CategoryIntents.Dir())
	inboxDir := filepath.Join(intentsDir, "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, "new-intent.md"), []byte("# New intent\n"), 0644); err != nil {
		t.Fatal(err)
	}

	older := buildTime.Add(-1 * time.Minute)
	if err := os.Chtimes(intentsDir, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(inboxDir, buildTime.Add(1*time.Minute), buildTime.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	idx := &Index{BuildTime: buildTime, Version: IndexVersion}
	if !IsStale(idx, root) {
		t.Error("Index should be stale when intent immediate-child topology is newer")
	}
}

func TestIsStale_FestivalsTopologyChanged(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	buildTime := time.Now()
	festivalsDir := filepath.Join(root, nav.CategoryFestivals.Dir())
	activeDir := filepath.Join(festivalsDir, "active")
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatal(err)
	}

	older := buildTime.Add(-1 * time.Minute)
	if err := os.Chtimes(festivalsDir, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(activeDir, buildTime.Add(1*time.Minute), buildTime.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	idx := &Index{BuildTime: buildTime, Version: IndexVersion}
	if !IsStale(idx, root) {
		t.Error("Index should be stale when festivals immediate-child topology is newer")
	}
}

func TestIsStale_DeepFileEditDoesNotInvalidate(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	buildTime := time.Now()
	workflowDir := filepath.Join(root, nav.CategoryWorkflow.Dir())
	designDir := filepath.Join(workflowDir, "design")
	workitemDir := filepath.Join(designDir, "existing-workitem")
	if err := os.MkdirAll(workitemDir, 0755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(workitemDir, "notes.md")
	if err := os.WriteFile(notePath, []byte("# Notes\n"), 0644); err != nil {
		t.Fatal(err)
	}

	older := buildTime.Add(-1 * time.Minute)
	for _, dir := range []string{workflowDir, designDir, workitemDir} {
		if err := os.Chtimes(dir, older, older); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(notePath, buildTime.Add(1*time.Minute), buildTime.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	idx := &Index{BuildTime: buildTime, Version: IndexVersion}
	if IsStale(idx, root) {
		t.Error("Index should not be stale for a deep file edit inside a workitem")
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

// TestGetOrBuild_DiscardsVersionMismatchedCache reproduces the stale-cache
// scenario: a campaign carries a nav index written by an older binary whose
// IndexVersion is behind the running binary. GetOrBuild must discard that cache
// and rebuild without a manual "camp cache rebuild", so campaigns self-heal
// after the enumeration semantics change.
func TestGetOrBuild_DiscardsVersionMismatchedCache(t *testing.T) {
	root := setupTestCampaign(t)
	ctx := context.Background()

	// A target a fresh build would never produce, tagged with the previous
	// index version. Everything else about the cache is fresh, so the version
	// mismatch is the only reason it can be considered stale.
	stale := &Index{
		Targets: []Target{{
			Name:     "ghost@stale-worktree",
			Path:     filepath.Join(root, "projects", "worktrees", "ghost", "stale-worktree"),
			Category: nav.CategoryWorktrees,
		}},
		BuildTime:    time.Now(),
		CampaignRoot: root,
		Version:      IndexVersion - 1,
	}
	if err := Save(stale, root); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	idx, err := GetOrBuild(ctx, root, false)
	if err != nil {
		t.Fatalf("GetOrBuild() error = %v", err)
	}

	if idx.Version != IndexVersion {
		t.Errorf("rebuilt index Version = %d, want %d", idx.Version, IndexVersion)
	}
	for _, target := range idx.Targets {
		if target.Name == "ghost@stale-worktree" {
			t.Fatalf("stale version-%d target survived; cache was not discarded", IndexVersion-1)
		}
	}

	// The self-heal must persist to disk at the current version.
	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded == nil || reloaded.Version != IndexVersion {
		t.Errorf("persisted cache = %v, want Version %d", reloaded, IndexVersion)
	}
}

func TestGetOrBuild_ForceRebuild(t *testing.T) {
	root := setupTestCampaign(t)

	ctx := context.Background()

	// First call builds
	idx1, _ := GetOrBuild(ctx, root, false)

	cachedBuildTime := idx1.BuildTime.Add(-time.Second)
	idx1.BuildTime = cachedBuildTime
	if err := Save(idx1, root); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	now := time.Now().Add(time.Second)
	if err := os.Chtimes(root, now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Force rebuild should create new index
	idx2, err := GetOrBuild(ctx, root, true)
	if err != nil {
		t.Fatalf("GetOrBuild() error = %v", err)
	}

	// Build time should come from the forced rebuild, not the cached value.
	if !idx2.BuildTime.After(cachedBuildTime) {
		t.Errorf("Force rebuild BuildTime = %v, want after cached %v", idx2.BuildTime, cachedBuildTime)
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
