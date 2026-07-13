package artifacts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/peer"
)

func TestFileAddRemoveValidate(t *testing.T) {
	f := &File{Version: 1}

	if err := f.Add(Root{Path: "media/renders"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := f.Add(Root{Path: "./media/renders/"}); err == nil {
		t.Error("Add() duplicate (unnormalized) accepted, want error")
	}
	if err := f.Add(Root{Path: "/abs/path"}); err == nil {
		t.Error("Add() absolute path accepted, want error")
	}
	if err := f.Add(Root{Path: "../escape"}); err == nil {
		t.Error("Add() escaping path accepted, want error")
	}
	if err := f.Add(Root{Path: ".campaign/cache"}); err == nil {
		t.Error("Add() .campaign path accepted, want error")
	}
	if err := f.Add(Root{Path: "datasets", Policy: "sometimes"}); err == nil {
		t.Error("Add() unknown policy accepted, want error")
	}
	if err := f.Add(Root{Path: "datasets", Policy: PolicyOnDemand}); err != nil {
		t.Fatalf("Add() on-demand error = %v", err)
	}

	if _, found := f.Find("media/renders"); !found {
		t.Error("Find() did not locate declared root")
	}
	if !f.Remove("media/renders/") {
		t.Error("Remove() = false, want true")
	}
	if _, found := f.Find("media/renders"); found {
		t.Error("Find() located removed root")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	campaignRoot := t.TempDir()
	f := &File{Version: 1}
	if err := f.Add(Root{Path: "media", Policy: PolicyOnDemand}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := f.Save(campaignRoot); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(campaignRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Roots) != 1 || loaded.Roots[0].Path != "media" || loaded.Roots[0].Policy != PolicyOnDemand {
		t.Errorf("Load() roots = %+v, want media/on-demand", loaded.Roots)
	}

	empty, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() missing file error = %v", err)
	}
	if len(empty.Roots) != 0 {
		t.Errorf("Load() missing file roots = %d, want 0", len(empty.Roots))
	}
}

func TestBuildManifestAndProtectedPaths(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()
	writeArtifact(t, campaignRoot, "media/a.bin", "aaa")
	writeArtifact(t, campaignRoot, "media/sub/b.bin", "bbbb")

	m, err := BuildManifest(ctx, campaignRoot, "media")
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("manifest files = %d, want 2", len(m.Files))
	}
	if m.Files[0].Path != "a.bin" || m.Files[1].Path != "sub/b.bin" {
		t.Errorf("manifest paths = %v, want sorted [a.bin sub/b.bin]", []string{m.Files[0].Path, m.Files[1].Path})
	}

	// Nil baseline: everything is protected.
	if got := m.ProtectedPaths(nil); len(got) != 2 {
		t.Errorf("ProtectedPaths(nil) = %v, want all files", got)
	}
	// Baseline identical: nothing protected.
	if got := m.ProtectedPaths(m); len(got) != 0 {
		t.Errorf("ProtectedPaths(self) = %v, want none", got)
	}
	// Baseline missing one file: that file is protected (never agreed).
	partial := &Manifest{Version: 1, Root: "media", Files: m.Files[:1]}
	if got := m.ProtectedPaths(partial); len(got) != 1 || got[0] != "sub/b.bin" {
		t.Errorf("ProtectedPaths(partial) = %v, want [sub/b.bin]", got)
	}

	// Missing root directory: empty manifest, no error.
	empty, err := BuildManifest(ctx, campaignRoot, "does-not-exist")
	if err != nil {
		t.Fatalf("BuildManifest() missing root error = %v", err)
	}
	if len(empty.Files) != 0 {
		t.Errorf("missing root manifest files = %d, want 0", len(empty.Files))
	}
}

func TestPullLifecycle(t *testing.T) {
	requireRsync(t)
	ctx := context.Background()

	peerCampaign := t.TempDir()
	localCampaign := t.TempDir()
	writeArtifact(t, peerCampaign, "media/render.mov", "peer render v1")
	writeArtifact(t, peerCampaign, "media/audio.wav", "peer audio v1")
	src := peer.FromPath("peerbox", peerCampaign)
	root := Root{Path: "media"}

	// First sync into an empty local root: everything arrives.
	r1 := Pull(ctx, localCampaign, src, root)
	if r1.Warning != "" || !r1.Synced {
		t.Fatalf("first pull = %+v, want clean sync", r1)
	}
	if !r1.FirstSync {
		t.Error("first pull FirstSync = false, want true")
	}
	assertArtifact(t, localCampaign, "media/render.mov", "peer render v1")

	// Peer updates a file; local is untouched: the update flows through.
	time.Sleep(1100 * time.Millisecond) // distinct mtime at unix-second resolution
	writeArtifact(t, peerCampaign, "media/render.mov", "peer render v2")
	r2 := Pull(ctx, localCampaign, src, root)
	if r2.Warning != "" || !r2.Synced || r2.FirstSync {
		t.Fatalf("second pull = %+v, want clean non-first sync", r2)
	}
	if len(r2.SkippedConflicts) != 0 {
		t.Errorf("second pull conflicts = %v, want none", r2.SkippedConflicts)
	}
	assertArtifact(t, localCampaign, "media/render.mov", "peer render v2")

	// Both sides change the same file: local wins, conflict reported, and the
	// protection is sticky on the following pull.
	time.Sleep(1100 * time.Millisecond)
	writeArtifact(t, localCampaign, "media/render.mov", "LOCAL edit")
	writeArtifact(t, peerCampaign, "media/render.mov", "peer render v3")
	r3 := Pull(ctx, localCampaign, src, root)
	if r3.Warning != "" || !r3.Synced {
		t.Fatalf("third pull = %+v, want sync with conflict", r3)
	}
	if len(r3.SkippedConflicts) != 1 || r3.SkippedConflicts[0] != "render.mov" {
		t.Errorf("third pull conflicts = %v, want [render.mov]", r3.SkippedConflicts)
	}
	assertArtifact(t, localCampaign, "media/render.mov", "LOCAL edit")

	r4 := Pull(ctx, localCampaign, src, root)
	if len(r4.SkippedConflicts) != 1 {
		t.Errorf("fourth pull conflicts = %v, want conflict to stay sticky", r4.SkippedConflicts)
	}
	assertArtifact(t, localCampaign, "media/render.mov", "LOCAL edit")

	// Resolution: remove the local file to take the peer's copy.
	if err := os.Remove(filepath.Join(localCampaign, "media", "render.mov")); err != nil {
		t.Fatalf("removing conflicted file: %v", err)
	}
	r5 := Pull(ctx, localCampaign, src, root)
	if r5.Warning != "" || len(r5.SkippedConflicts) != 0 {
		t.Fatalf("fifth pull = %+v, want resolved", r5)
	}
	assertArtifact(t, localCampaign, "media/render.mov", "peer render v3")
}

func TestPullProtectsPreexistingLocalFiles(t *testing.T) {
	requireRsync(t)
	ctx := context.Background()

	peerCampaign := t.TempDir()
	localCampaign := t.TempDir()
	writeArtifact(t, peerCampaign, "media/shared.bin", "peer version")
	writeArtifact(t, localCampaign, "media/shared.bin", "precious local version")

	src := peer.FromPath("peerbox", peerCampaign)
	r1 := Pull(ctx, localCampaign, src, Root{Path: "media"})
	if r1.Warning != "" || !r1.Synced || !r1.FirstSync {
		t.Fatalf("first pull = %+v, want clean first sync", r1)
	}
	if r1.Protected != 1 {
		t.Errorf("first pull Protected = %d, want 1", r1.Protected)
	}
	assertArtifact(t, localCampaign, "media/shared.bin", "precious local version")

	// Still protected on the next pull: it was never agreed state.
	r2 := Pull(ctx, localCampaign, src, Root{Path: "media"})
	if r2.Protected != 1 {
		t.Errorf("second pull Protected = %d, want 1 (protection must persist)", r2.Protected)
	}
	assertArtifact(t, localCampaign, "media/shared.bin", "precious local version")
}

func TestPullUnreachablePeerWarns(t *testing.T) {
	requireRsync(t)
	ctx := context.Background()
	localCampaign := t.TempDir()
	src := peer.FromPath("ghost", filepath.Join(t.TempDir(), "missing"))

	r := Pull(ctx, localCampaign, src, Root{Path: "media"})
	if r.Synced {
		t.Error("pull from missing peer Synced = true, want false")
	}
	if r.Warning == "" {
		t.Error("pull from missing peer Warning empty, want message")
	}
}

func TestVerifyAgainstSnapshot(t *testing.T) {
	requireRsync(t)
	ctx := context.Background()

	peerCampaign := t.TempDir()
	localCampaign := t.TempDir()
	writeArtifact(t, peerCampaign, "media/a.bin", "aaa")
	writeArtifact(t, peerCampaign, "media/b.bin", "bbb")
	src := peer.FromPath("peerbox", peerCampaign)

	if r := Pull(ctx, localCampaign, src, Root{Path: "media"}); r.Warning != "" {
		t.Fatalf("pull warning: %s", r.Warning)
	}

	local, err := BuildManifest(ctx, localCampaign, "media")
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	snapshot, err := LoadSnapshot(localCampaign, "peerbox", "media")
	if err != nil || snapshot == nil {
		t.Fatalf("LoadSnapshot() = %v, %v; want snapshot", snapshot, err)
	}
	if v := Verify(local, snapshot); !v.Clean() {
		t.Errorf("Verify() after pull = %+v, want clean", v)
	}

	if err := os.Remove(filepath.Join(localCampaign, "media", "b.bin")); err != nil {
		t.Fatalf("removing file: %v", err)
	}
	local, err = BuildManifest(ctx, localCampaign, "media")
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	v := Verify(local, snapshot)
	if len(v.Missing) != 1 || v.Missing[0] != "b.bin" {
		t.Errorf("Verify() missing = %v, want [b.bin]", v.Missing)
	}
}

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not available")
	}
}

func writeArtifact(t *testing.T, campaignRoot, rel, content string) {
	t.Helper()
	path := filepath.Join(campaignRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func assertArtifact(t *testing.T, campaignRoot, rel, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(campaignRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	if string(data) != want {
		t.Errorf("%s = %q, want %q", rel, string(data), want)
	}
}
