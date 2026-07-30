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
	"time"
)

func buildHashedManifest(t *testing.T, campaignRoot, rootRel string, prev *Manifest) *Manifest {
	t.Helper()
	m, err := BuildManifest(context.Background(), campaignRoot, rootRel)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if _, err := HashManifest(context.Background(), campaignRoot, m, prev, nil); err != nil {
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

	if _, err := HashManifest(context.Background(), t.TempDir(), m, prev, nil); err != nil {
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
	_, err = HashManifest(context.Background(), root, m, nil, func(done int64) {
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
	if _, err := HashManifest(ctx, root, m, nil, nil); err == nil {
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

	if _, err := HashManifest(context.Background(), root, m, nil, nil); err != nil {
		t.Fatalf("a vanished file must not fail the manifest: %v", err)
	}
	if got := m.Index()["gone.bin"].HashSHA256; got != "" {
		t.Errorf("vanished file hash = %q, want unknown", got)
	}
}

// A manifest entry must never pair a stat from before the read with a hash from
// after it: that record describes a state that never existed at any single
// moment, and nothing downstream can tell it apart from a true one. The
// progress callback fires mid-read, which is the one place a test can reach
// inside the hash window.
func TestHashManifestEntryStatDescribesTheBytesItHashed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "growing.bin")
	original := bytes.Repeat([]byte("a"), 4096)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := BuildManifest(context.Background(), root, "media")
	if err != nil {
		t.Fatal(err)
	}
	walked := m.Index()["growing.bin"]

	// Grow the file once, from inside the first read.
	grown := append(bytes.Repeat([]byte("a"), 4096), bytes.Repeat([]byte("b"), 4096)...)
	var mutated bool
	outcome, err := HashManifest(context.Background(), root, m, nil, func(int64) {
		if mutated {
			return
		}
		mutated = true
		if werr := os.WriteFile(path, grown, 0o644); werr != nil {
			t.Errorf("mutate during hash: %v", werr)
		}
	})
	if err != nil {
		t.Fatalf("hash manifest: %v", err)
	}
	if !mutated {
		t.Fatal("the progress callback never fired, so the hash window was never exercised")
	}

	entry := m.Index()["growing.bin"]
	if entry.Size == walked.Size && entry.MTime == walked.MTime {
		t.Fatal("entry kept the pre-read stat after the file changed under the reader")
	}

	// Whatever the entry ended up claiming, its stat and its hash must describe
	// one observation. An unknown hash is the honest answer for a file that
	// would not settle; a hash present must be the hash of the bytes the stat
	// describes.
	if entry.HashSHA256 == "" {
		if len(outcome.Unsettled) != 1 || outcome.Unsettled[0] != "growing.bin" {
			t.Errorf("an unhashed entry must be reported as unsettled, got %v", outcome.Unsettled)
		}
		return
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(onDisk)
	if entry.HashSHA256 != hex.EncodeToString(want[:]) {
		t.Errorf("hash = %q, want the hash of the bytes the entry's stat describes", entry.HashSHA256)
	}
	if int64(len(onDisk)) != entry.Size {
		t.Errorf("entry size = %d, want the size of the hashed bytes %d", entry.Size, len(onDisk))
	}
}

// A file under continuous write never settles. The record must then say the
// hash is unknown rather than pair a stat with a hash it does not belong to,
// and it must say so out loud instead of looking complete.
func TestHashManifestReportsAFileItCouldNotPinDown(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "streaming.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 2048), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := BuildManifest(context.Background(), root, "media")
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite in place on every read, so every settle attempt observes a file
	// that moved under it. The size stays fixed and the mtime is stamped
	// explicitly: a growing file would outrun the reader and never reach EOF,
	// and a same-size rewrite is the harder case anyway (only the mtime betrays
	// it). One callback fires per read here, the content being far under the
	// 1MiB chunk.
	stamp := time.Now()
	rewrites := 0
	outcome, err := HashManifest(context.Background(), root, m, nil, func(int64) {
		rewrites++
		if werr := os.WriteFile(path, bytes.Repeat([]byte("b"), 2048), 0o644); werr != nil {
			t.Errorf("mutate during hash: %v", werr)
			return
		}
		stamp = stamp.Add(time.Second)
		if werr := os.Chtimes(path, stamp, stamp); werr != nil {
			t.Errorf("stamp mtime during hash: %v", werr)
		}
	})
	if err != nil {
		t.Fatalf("a file under continuous write must not fail the manifest: %v", err)
	}
	if rewrites < settleAttempts {
		t.Fatalf("only %d read(s) happened; the settle loop was not exhausted", rewrites)
	}
	if got := m.Index()["streaming.bin"].HashSHA256; got != "" {
		t.Errorf("hash = %q, want unknown for a file that never settled", got)
	}
	if len(outcome.Unsettled) != 1 || outcome.Unsettled[0] != "streaming.bin" {
		t.Errorf("Unsettled = %v, want the file that never settled", outcome.Unsettled)
	}
}

// The settled path must leave the ordinary contract alone: one read, the walk's
// stat preserved, and nothing reported.
func TestHashManifestReportsNothingWhenTheRootIsStill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "still.bin"), []byte("stable bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := BuildManifest(context.Background(), root, "media")
	if err != nil {
		t.Fatal(err)
	}
	walked := m.Index()["still.bin"]

	outcome, err := HashManifest(context.Background(), root, m, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Unsettled) != 0 {
		t.Errorf("Unsettled = %v, want nothing for a still root", outcome.Unsettled)
	}
	entry := m.Index()["still.bin"]
	if entry.Size != walked.Size || entry.MTime != walked.MTime {
		t.Errorf("a still file's stat moved: walked %+v, recorded %+v", walked, entry)
	}
	want := sha256.Sum256([]byte("stable bytes"))
	if entry.HashSHA256 != hex.EncodeToString(want[:]) {
		t.Errorf("hash = %q, want the file's sha256", entry.HashSHA256)
	}
}

func TestDecodeCommittedRefusesAMismatchedRoot(t *testing.T) {
	data := []byte(`{"version":1,"root":"other","describes_commit":"abc","files":[{"path":"f","size":1,"mtime_unix_nano":1,"hash_sha256":"aa"}]}`)
	m, describes, err := decodeCommitted(data, "media")
	if err != nil || m != nil || describes != "" {
		t.Errorf("a record embedding another root must degrade to first pass, got m=%v describes=%q err=%v", m, describes, err)
	}
}

func TestFingerprintFilesChangesWithStatState(t *testing.T) {
	a := []FileEntry{{Path: "f", Size: 1, MTime: 10}}
	same := []FileEntry{{Path: "f", Size: 1, MTime: 10}}
	moved := []FileEntry{{Path: "f", Size: 1, MTime: 11}}
	if FingerprintFiles(a) != FingerprintFiles(same) {
		t.Error("identical stat state must fingerprint identically")
	}
	if FingerprintFiles(a) == FingerprintFiles(moved) {
		t.Error("a moved mtime must change the fingerprint")
	}
}
