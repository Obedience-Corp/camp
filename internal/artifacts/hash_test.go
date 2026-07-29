package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func buildHashedManifest(t *testing.T, campaignRoot, rootRel string, prev *Manifest) *Manifest {
	t.Helper()
	m, err := BuildManifest(context.Background(), campaignRoot, rootRel)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if err := HashManifest(context.Background(), campaignRoot, m, prev, nil); err != nil {
		t.Fatalf("hash manifest: %v", err)
	}
	return m
}

func TestHashManifestHashesRegularFilesAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("the bytes a backup tool must reproduce")
	if err := os.WriteFile(filepath.Join(dir, "clip.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("clip.bin", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	m := buildHashedManifest(t, root, "media", nil)

	want := sha256.Sum256(content)
	byPath := m.Index()
	if got := byPath["clip.bin"].HashSHA256; got != hex.EncodeToString(want[:]) {
		t.Errorf("clip.bin hash = %q, want the file's sha256", got)
	}
	if got := byPath["link"].HashSHA256; got != "" {
		t.Errorf("symlink carried a content hash %q; its identity is kind+path", got)
	}
}

func TestHashManifestCarriesForwardWithoutReading(t *testing.T) {
	// The entries exist only in the manifests; no file backs them. A
	// carry-forward that read from disk would fail loudly here, which is the
	// point: an unchanged entry must inherit its hash from prev with no read.
	prev := &Manifest{Root: "media", Files: []FileEntry{
		{Path: "steady.bin", Size: 9, MTime: 1234, HashSHA256: "cafe"},
	}}
	m := &Manifest{Root: "media", Files: []FileEntry{
		{Path: "steady.bin", Size: 9, MTime: 1234},
	}}

	if err := HashManifest(context.Background(), t.TempDir(), m, prev, nil); err != nil {
		t.Fatalf("hash manifest: %v", err)
	}
	if got := m.Files[0].HashSHA256; got != "cafe" {
		t.Errorf("unchanged entry hash = %q, want carried-forward %q", got, "cafe")
	}
}

func TestHashManifestRehashesWhatMoved(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "clip.bin")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := buildHashedManifest(t, root, "media", nil)

	if err := os.WriteFile(path, []byte("after, and longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := buildHashedManifest(t, root, "media", first)

	want := sha256.Sum256([]byte("after, and longer"))
	if got := second.Index()["clip.bin"].HashSHA256; got != hex.EncodeToString(want[:]) {
		t.Errorf("changed entry hash = %q, want re-hashed content", got)
	}

	// A pre-hashing manifest degrades to re-hash: same shape, hash absent.
	var legacy Manifest
	if err := json.Unmarshal([]byte(`{"version":1,"root":"media","files":[{"path":"clip.bin","size":6,"mtime_unix_nano":1}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Files[0].HashSHA256 != "" {
		t.Error("a manifest written before hashing existed must unmarshal to hash unknown")
	}
}

func TestCommittedJSONIsByteIdenticalWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clip.bin"), []byte("stable"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := buildHashedManifest(t, root, "media", nil)
	second := buildHashedManifest(t, root, "media", first)

	a, err := first.CommittedJSON("abc123")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.CommittedJSON("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("unchanged root produced differing committed bytes:\n%s\n---\n%s", a, b)
	}
	if bytes.Contains(a, []byte("generated_at")) {
		t.Error("committed form must not carry generated_at; it would churn every commit")
	}
	if !bytes.Contains(a, []byte(`"describes_commit": "abc123"`)) {
		t.Error("committed form must record the commit it describes")
	}
	if !SameFiles(first.Files, second.Files) {
		t.Error("unchanged root must compare as same files; this is the skip-commit decision")
	}
}

func TestHashManifestReportsProgressAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(filepath.Join(dir, "clip.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := BuildManifest(context.Background(), root, "media")
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	calls := 0
	err = HashManifest(context.Background(), root, m, nil, func(done int64) {
		if done < last {
			t.Errorf("progress went backwards: %d after %d", done, last)
		}
		last = done
		calls++
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || last != int64(len(content)) {
		t.Errorf("progress calls=%d last=%d, want coverage of all %d bytes", calls, last, len(content))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := HashManifest(ctx, root, m, nil, nil); err == nil {
		t.Error("cancelled context must stop the hash pass with an error")
	}
}

func TestHashManifestToleratesAVanishedFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.bin"), []byte("temp"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(context.Background(), root, "media")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "gone.bin")); err != nil {
		t.Fatal(err)
	}

	if err := HashManifest(context.Background(), root, m, nil, nil); err != nil {
		t.Fatalf("a vanished file must not fail the manifest: %v", err)
	}
	if got := m.Index()["gone.bin"].HashSHA256; got != "" {
		t.Errorf("vanished file hash = %q, want unknown", got)
	}
}
