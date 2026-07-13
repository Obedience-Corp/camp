package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/peer"
)

func TestSync_CleanRepo(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true for clean repo")
	}
	if !result.PreflightPassed {
		t.Error("Sync().PreflightPassed = false, want true")
	}
}

func TestSync_DryRun(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Create a URL mismatch
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	syncer := NewSyncer(repoRoot, WithDryRun(true))
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true in dry-run mode")
	}

	// Should report the URL change that would happen
	if len(result.URLChanges) != 1 {
		t.Errorf("URLChanges = %d, want 1 in dry-run mode", len(result.URLChanges))
	}

	// Verify the URL wasn't actually changed
	cmd := exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	output, _ := cmd.Output()
	if string(output) != "https://different.url/repo.git\n" {
		t.Error("dry-run mode should not modify URLs")
	}
}

func TestSync_ForceMode(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Create uncommitted changes
	createFile(t, filepath.Join(subPath, "dirty.txt"), "dirty content")

	// Without force, should fail
	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Success {
		t.Error("Sync().Success = true, want false without force mode")
	}

	// With force, should succeed
	syncer = NewSyncer(repoRoot, WithForce(true))
	result, err = syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !result.Success {
		t.Error("Sync().Success = false, want true with force mode")
	}

	// Should have warnings about uncommitted changes
	hasWarning := false
	for _, w := range result.Warnings {
		if contains(w, "uncommitted") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected warning about uncommitted changes in force mode")
	}
}

func TestSync_URLMismatch(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Get the original URL
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	originalURL, _ := cmd.Output()

	// Create a URL mismatch by changing .git/config
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://different.url/repo.git")

	syncer := NewSyncer(repoRoot)
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true")
	}

	// Should have fixed the URL
	cmd = exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	newURL, _ := cmd.Output()
	if string(newURL) != string(originalURL) {
		t.Errorf("URL not synced: got %q, want %q", string(newURL), string(originalURL))
	}
}

func TestSync_ParallelJobs(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub1")
	setupSubmodule(t, repoRoot, "projects/sub2")

	syncer := NewSyncer(repoRoot, WithParallel(4))
	result, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Success {
		t.Error("Sync().Success = false, want true")
	}

	// Both submodules should be in results
	if len(result.UpdateResults) != 2 {
		t.Errorf("UpdateResults = %d, want 2", len(result.UpdateResults))
	}
}

func TestUpdateSubmodules_PreservesDeclarationOrder(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	want := []string{"projects/sub1", "projects/sub2", "projects/sub3", "projects/sub4"}
	for _, p := range want {
		setupSubmodule(t, repoRoot, p)
	}

	syncer := NewSyncer(repoRoot, WithParallel(2))
	results, err := syncer.updateSubmodules(ctx)
	if err != nil {
		t.Fatalf("updateSubmodules() error = %v", err)
	}

	if len(results) != len(want) {
		t.Fatalf("updateSubmodules() results = %d, want %d", len(results), len(want))
	}
	for i, r := range results {
		if r.Path != want[i] {
			t.Errorf("results[%d].Path = %q, want %q", i, r.Path, want[i])
		}
		if !r.Success {
			t.Errorf("results[%d] (%s) Success = false, want true: %v", i, r.Path, r.Error)
		}
	}
}

func TestUpdateSubmodules_PartialFailureIsolation(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/healthy")
	setupSubmodule(t, repoRoot, "projects/broken")

	// Break the second submodule: deinit it, drop its module gitdir, and
	// remove its source repo so re-init has nowhere to clone from.
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/broken.url")
	urlBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading broken submodule url: %v", err)
	}
	sourceRepo := strings.TrimSpace(string(urlBytes))
	runGit(t, repoRoot, "submodule", "deinit", "-f", "projects/broken")
	if err := os.RemoveAll(filepath.Join(repoRoot, ".git", "modules", "projects/broken")); err != nil {
		t.Fatalf("removing module gitdir: %v", err)
	}
	if err := os.RemoveAll(sourceRepo); err != nil {
		t.Fatalf("removing source repo: %v", err)
	}

	syncer := NewSyncer(repoRoot, WithParallel(2))
	results, err := syncer.updateSubmodules(ctx)
	if err != nil {
		t.Fatalf("updateSubmodules() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("updateSubmodules() results = %d, want 2", len(results))
	}
	byPath := make(map[string]SubmoduleResult, len(results))
	for _, r := range results {
		byPath[r.Path] = r
	}
	if !byPath["projects/healthy"].Success {
		t.Errorf("healthy submodule Success = false, want true: %v", byPath["projects/healthy"].Error)
	}
	broken := byPath["projects/broken"]
	if broken.Success {
		t.Error("broken submodule Success = true, want false")
	}
	if broken.Error == nil {
		t.Fatal("broken submodule Error = nil, want error")
	}
	syncErr, ok := broken.Error.(*SyncError)
	if !ok {
		t.Fatalf("broken submodule error type = %T, want *SyncError", broken.Error)
	}
	if syncErr.Op != "init" {
		t.Errorf("SyncError.Op = %q, want %q", syncErr.Op, "init")
	}
}

// Mid-flight cancellation must abort promptly through the concurrent paths
// (spawn-loop break, semaphore-wait select, in-flight git subprocesses) and
// still honor the contract of returning (nil, context.Canceled). Six
// submodules at parallelism 1 take well over a second of git work, so a
// 150ms cancel reliably lands while workers are in flight; if timing ever
// degenerates to cancel-before-entry, the assertions still hold via the
// entry guard.
func TestUpdateSubmodules_ContextCanceledMidFlight(t *testing.T) {
	repoRoot := setupTestRepo(t)
	for i := range 6 {
		setupSubmodule(t, repoRoot, fmt.Sprintf("projects/sub%d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	syncer := NewSyncer(repoRoot, WithParallel(1))

	type outcome struct {
		results []SubmoduleResult
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := syncer.updateSubmodules(ctx)
		done <- outcome{results, err}
	}()

	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("updateSubmodules() error = %v, want context.Canceled", got.err)
		}
		if got.results != nil {
			t.Errorf("updateSubmodules() results = %v, want nil on cancellation", got.results)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("updateSubmodules() did not return after cancellation")
	}
}

func TestUpdateSubmodules_PeerFetch(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Build a peer campaign mirroring the layout: projects/sub is a clone of
	// the same source repo with a branch the local copy has never seen.
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	urlBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading submodule url: %v", err)
	}
	sourceRepo := strings.TrimSpace(string(urlBytes))

	peerRoot := t.TempDir()
	peerSub := filepath.Join(peerRoot, "projects", "sub")
	runGit(t, t.TempDir(), "clone", sourceRepo, peerSub)
	runGit(t, peerSub, "config", "user.email", "test@test.com")
	runGit(t, peerSub, "config", "user.name", "Test")
	runGit(t, peerSub, "checkout", "-b", "peer-work")
	createFile(t, filepath.Join(peerSub, "peer-only.txt"), "only on the peer")
	runGit(t, peerSub, "add", ".")
	runGit(t, peerSub, "commit", "-m", "peer-only work")

	syncer := NewSyncer(repoRoot, WithPeer(peer.FromPath("peerbox", peerRoot)))
	results, err := syncer.updateSubmodules(ctx)
	if err != nil {
		t.Fatalf("updateSubmodules() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("updateSubmodules() results = %d, want 1", len(results))
	}
	if !results[0].Success {
		t.Fatalf("submodule Success = false: %v", results[0].Error)
	}
	if !results[0].PeerFetched {
		t.Errorf("PeerFetched = false, want true (warning: %q)", results[0].PeerWarning)
	}

	// Peer heads land under refs/peer/<id>/*, bringing their objects along.
	localSub := filepath.Join(repoRoot, "projects/sub")
	verify := exec.Command("git", "-C", localSub, "rev-parse", "--verify", "refs/peer/peerbox/peer-work")
	if out, verifyErr := verify.CombinedOutput(); verifyErr != nil {
		t.Errorf("refs/peer/peerbox/peer-work not present after peer fetch: %s", strings.TrimSpace(string(out)))
	}
}

func TestUpdateSubmodules_PeerUnreachableDegrades(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot, WithPeer(peer.FromPath("ghost", filepath.Join(t.TempDir(), "missing"))))
	results, err := syncer.updateSubmodules(ctx)
	if err != nil {
		t.Fatalf("updateSubmodules() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("updateSubmodules() results = %d, want 1", len(results))
	}
	if !results[0].Success {
		t.Errorf("submodule Success = false, want true (peer failure must degrade): %v", results[0].Error)
	}
	if results[0].PeerFetched {
		t.Error("PeerFetched = true, want false for unreachable peer")
	}
	if results[0].PeerWarning == "" {
		t.Error("PeerWarning empty, want degradation warning")
	}
}

// Peer submodule sitting detached at a commit that is not a branch tip: HEAD
// must still transfer so gitlink objects accelerate without origin.
func TestUpdateSubmodules_PeerFetchDetachedHEAD(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	urlBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading submodule url: %v", err)
	}
	sourceRepo := strings.TrimSpace(string(urlBytes))

	peerRoot := t.TempDir()
	peerSub := filepath.Join(peerRoot, "projects", "sub")
	runGit(t, t.TempDir(), "clone", sourceRepo, peerSub)
	runGit(t, peerSub, "config", "user.email", "test@test.com")
	runGit(t, peerSub, "config", "user.name", "Test")
	// Detached commit reachable only via HEAD (delete the only branch tip).
	runGit(t, peerSub, "checkout", "--detach")
	createFile(t, filepath.Join(peerSub, "detached-only.txt"), "only on detached peer HEAD")
	runGit(t, peerSub, "add", ".")
	runGit(t, peerSub, "commit", "-m", "detached peer tip")
	shaOut, err := exec.Command("git", "-C", peerSub, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("peer HEAD: %v", err)
	}
	detachedSHA := strings.TrimSpace(string(shaOut))
	// Drop all local branches so heads refspec alone would miss this object.
	branches, err := exec.Command("git", "-C", peerSub, "for-each-ref", "--format=%(refname:short)", "refs/heads").Output()
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	for _, b := range strings.Fields(string(branches)) {
		runGit(t, peerSub, "branch", "-D", b)
	}

	syncer := NewSyncer(repoRoot, WithPeer(peer.FromPath("peerbox", peerRoot)))
	results, err := syncer.updateSubmodules(ctx)
	if err != nil {
		t.Fatalf("updateSubmodules() error = %v", err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("update failed: %+v", results)
	}
	if !results[0].PeerFetched {
		t.Fatalf("PeerFetched = false (warning: %q)", results[0].PeerWarning)
	}

	localSub := filepath.Join(repoRoot, "projects/sub")
	if out, verifyErr := exec.Command("git", "-C", localSub, "cat-file", "-e", detachedSHA).CombinedOutput(); verifyErr != nil {
		t.Errorf("detached peer HEAD object %s missing after peer fetch: %s",
			detachedSHA, strings.TrimSpace(string(out)))
	}
	if out, verifyErr := exec.Command("git", "-C", localSub, "rev-parse", "--verify", "refs/peer/peerbox/HEAD").CombinedOutput(); verifyErr != nil {
		t.Errorf("refs/peer/peerbox/HEAD not present: %s", strings.TrimSpace(string(out)))
	}
}

func TestUpdateSubmodules_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	_, err := syncer.updateSubmodules(ctx)
	if err != context.Canceled {
		t.Errorf("updateSubmodules() error = %v, want context.Canceled", err)
	}
}

func TestSync_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	syncer := NewSyncer(repoRoot)

	_, err := syncer.Sync(ctx)
	if err != context.Canceled {
		t.Errorf("Sync() error = %v, want context.Canceled", err)
	}
}

func TestSyncURLs(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	// Get the original URL from .gitmodules
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	declaredURL, _ := cmd.Output()

	// Create a URL mismatch
	runGit(t, repoRoot, "config", "submodule.projects/sub.url", "https://old.url/repo.git")

	syncer := NewSyncer(repoRoot)
	changes, err := syncer.syncURLs(ctx)
	if err != nil {
		t.Fatalf("syncURLs() error = %v", err)
	}

	// Should detect the URL change
	if len(changes) != 1 {
		t.Fatalf("syncURLs() changes = %d, want 1", len(changes))
	}

	if changes[0].OldURL != "https://old.url/repo.git" {
		t.Errorf("OldURL = %q, want %q", changes[0].OldURL, "https://old.url/repo.git")
	}

	// Verify URL was actually synced
	cmd = exec.Command("git", "-C", repoRoot, "config", "submodule.projects/sub.url")
	activeURL, _ := cmd.Output()
	if string(activeURL) != string(declaredURL) {
		t.Errorf("URL not synced: got %q, want %q", string(activeURL), string(declaredURL))
	}
}

func TestValidateUpdate_AllInitialized(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)

	// First do a proper sync to ensure everything is initialized
	_, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Now validate should pass
	err = syncer.validateUpdate(ctx)
	if err != nil {
		t.Errorf("validateUpdate() error = %v, want nil", err)
	}
}

func TestValidateSubmoduleStatusOutputPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
		wantOp  string
	}{
		{
			name:   "clean",
			output: " abc1234 projects/foo (heads/main)",
		},
		{
			name:    "not initialized",
			output:  "-abc1234 projects/foo",
			wantErr: true,
			wantOp:  "validate",
		},
		{
			name:    "drift",
			output:  "+abc1234 projects/foo (heads/main)",
			wantErr: true,
			wantOp:  "validate-drift",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubmoduleStatusOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatal("validateSubmoduleStatusOutput() error = nil, want error")
				}
				syncErr, ok := err.(*SyncError)
				if !ok {
					t.Fatalf("validateSubmoduleStatusOutput() error type = %T, want *SyncError", err)
				}
				if syncErr.Op != tt.wantOp {
					t.Errorf("SyncError.Op = %q, want %q", syncErr.Op, tt.wantOp)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSubmoduleStatusOutput() error = %v, want nil", err)
			}
		})
	}
}

func TestVerifySubmodules(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)

	// Run sync first to initialize
	_, _ = syncer.Sync(ctx)

	results, err := syncer.verifySubmodules(ctx)
	if err != nil {
		t.Fatalf("verifySubmodules() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("verifySubmodules() results = %d, want 1", len(results))
	}

	if !results[0].Success {
		t.Errorf("submodule Success = false, want true")
	}
}

func TestCollectWarnings(t *testing.T) {
	syncer := NewSyncer("/tmp/test", WithForce(true))

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
		DetachedHEADs: []DetachedHEADStatus{
			{Path: "projects/detached", LocalCommits: 3, HasLocalWork: true},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 2 {
		t.Fatalf("collectWarnings() = %d warnings, want 2", len(warnings))
	}

	// Check for uncommitted changes warning
	hasUncommitted := false
	hasDetached := false
	for _, w := range warnings {
		if contains(w, "uncommitted") && contains(w, "projects/dirty") {
			hasUncommitted = true
		}
		if contains(w, "detached HEAD") && contains(w, "projects/detached") {
			hasDetached = true
		}
	}

	if !hasUncommitted {
		t.Error("expected uncommitted changes warning")
	}
	if !hasDetached {
		t.Error("expected detached HEAD warning")
	}
}

func TestCollectWarnings_SafeMode(t *testing.T) {
	// In safe mode (no force), uncommitted changes should NOT become warnings
	// because they cause the sync to abort
	syncer := NewSyncer("/tmp/test") // No force

	preflight := &PreflightResult{
		UncommittedChanges: []SubmoduleStatus{
			{Path: "projects/dirty", Details: "2 files"},
		},
	}

	warnings := syncer.collectWarnings(preflight)

	if len(warnings) != 0 {
		t.Errorf("collectWarnings() in safe mode = %d warnings, want 0", len(warnings))
	}
}

func TestReverseSyncLocalURLs_NoLocalPaths(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	setupSubmodule(t, repoRoot, "projects/sub")

	syncer := NewSyncer(repoRoot)
	changes, err := syncer.reverseSyncLocalURLs(ctx)
	if err != nil {
		t.Fatalf("reverseSyncLocalURLs() error = %v", err)
	}

	// The submodule was added from a temp dir (local path), but the submodule
	// itself has a remote origin pointing to that same temp dir. Since the
	// remote origin is also a local path, it should be skipped.
	// This is a no-op scenario because the remote URL is also local.
	for _, c := range changes {
		t.Logf("change: %s -> %s", c.OldURL, c.NewURL)
	}
}

func TestReverseSyncLocalURLs_LocalPathWithRemote(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Verify .gitmodules has a local path (from setupSubmodule using temp dir)
	cmd := exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	declaredBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get declared URL: %v", err)
	}
	declared := strings.TrimSpace(string(declaredBytes))
	t.Logf("declared URL before reverse sync: %s", declared)

	// Add a real remote origin inside the submodule
	remoteURL := "git@github.com:test-org/sub.git"
	runGit(t, subPath, "remote", "set-url", "origin", remoteURL)

	syncer := NewSyncer(repoRoot)
	changes, err := syncer.reverseSyncLocalURLs(ctx)
	if err != nil {
		t.Fatalf("reverseSyncLocalURLs() error = %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("reverseSyncLocalURLs() changes = %d, want 1", len(changes))
	}

	if changes[0].NewURL != remoteURL {
		t.Errorf("NewURL = %q, want %q", changes[0].NewURL, remoteURL)
	}

	// Verify .gitmodules was updated
	cmd = exec.Command("git", "-C", repoRoot, "config", "-f", ".gitmodules", "submodule.projects/sub.url")
	updatedBytes, _ := cmd.Output()
	updated := strings.TrimSpace(string(updatedBytes))
	if updated != remoteURL {
		t.Errorf(".gitmodules URL = %q, want %q", updated, remoteURL)
	}
}

func TestReverseSyncLocalURLs_LocalPathWithoutRemote(t *testing.T) {
	ctx := context.Background()
	repoRoot := setupTestRepo(t)
	subPath := setupSubmodule(t, repoRoot, "projects/sub")

	// Remove origin remote from the submodule
	runGit(t, subPath, "remote", "remove", "origin")

	syncer := NewSyncer(repoRoot)
	changes, err := syncer.reverseSyncLocalURLs(ctx)
	if err != nil {
		t.Fatalf("reverseSyncLocalURLs() error = %v", err)
	}

	// Should skip - no remote origin to sync from
	if len(changes) != 0 {
		t.Errorf("reverseSyncLocalURLs() changes = %d, want 0 (no remote origin)", len(changes))
	}
}

func TestReverseSyncLocalURLs_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoRoot := setupTestRepo(t)
	syncer := NewSyncer(repoRoot)

	_, err := syncer.reverseSyncLocalURLs(ctx)
	if err != context.Canceled {
		t.Errorf("reverseSyncLocalURLs() error = %v, want context.Canceled", err)
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
