//go:build container_fs

package notice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
)

const (
	testRoot    = "media/renders"
	thisMachine = "this-box"
	neverSynced = "has never synced"
)

// stageRoot declares testRoot in a fresh campaign, puts one file in it, and
// pins this machine's manifest identity.
func stageRoot(t *testing.T) string {
	t.Helper()
	t.Setenv(artifacts.EnvMachineName, thisMachine)
	campaignRoot := t.TempDir()
	cfg := &artifacts.File{Version: 1, Roots: []artifacts.Root{{Path: testRoot}}}
	if err := cfg.Save(campaignRoot); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(campaignRoot, filepath.FromSlash(testRoot))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return campaignRoot
}

func file(path string, size int64, hash string) artifacts.FileEntry {
	return artifacts.FileEntry{Path: path, Size: size, MTime: 1, HashSHA256: hash}
}

func record(files ...artifacts.FileEntry) *artifacts.Manifest {
	return &artifacts.Manifest{Version: 1, Root: testRoot, Files: files}
}

func commitManifest(t *testing.T, campaignRoot, machine string, m *artifacts.Manifest) {
	t.Helper()
	if _, err := artifacts.WriteCommitted(campaignRoot, machine, m, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
}

func saveSnapshot(t *testing.T, campaignRoot, peer string, m *artifacts.Manifest) {
	t.Helper()
	if err := artifacts.SaveSnapshot(campaignRoot, peer, testRoot, m); err != nil {
		t.Fatal(err)
	}
}

func detectNeverSynced(t *testing.T, campaignRoot string) *Notice {
	t.Helper()
	n, err := ArtifactRootNeverSynced(context.Background(), campaignRoot)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func requireNotice(t *testing.T, campaignRoot, wantMessage string) {
	t.Helper()
	n := detectNeverSynced(t, campaignRoot)
	if n == nil {
		t.Fatal("expected the never-synced notice")
	}
	if !strings.Contains(n.Message, wantMessage) {
		t.Errorf("Message = %q, want it to contain %q", n.Message, wantMessage)
	}
}

func requireNoNotice(t *testing.T, campaignRoot string) {
	t.Helper()
	if n := detectNeverSynced(t, campaignRoot); n != nil {
		t.Fatalf("expected no notice, got %+v", n)
	}
}

func TestNeverSyncedNamesTheRemedyFromAnotherMachine(t *testing.T) {
	campaignRoot := stageRoot(t)

	n := detectNeverSynced(t, campaignRoot)
	if n == nil {
		t.Fatal("a root with no second copy must notify")
	}
	if want := SubjectID(KindNeverSynced, testRoot); n.ID != want {
		t.Errorf("ID = %q, want %q", n.ID, want)
	}
	if n.Subject != testRoot {
		t.Errorf("Subject = %q, want %q", n.Subject, testRoot)
	}
	for _, want := range []string{
		"on another machine: camp sync --from <id of " + thisMachine + "> --artifacts-only",
		"camp notify dismiss " + n.ID,
	} {
		if !strings.Contains(n.Command, want) {
			t.Errorf("Command %q missing %q", n.Command, want)
		}
	}
}

// Without this machine's own record there is nothing to measure coverage
// against, so even a populated peer record leaves the notice up.
func TestNeverSyncedStaysWithoutThisMachinesManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-a")))
	saveSnapshot(t, campaignRoot, "laptop", record(file("a.bin", 1, "")))

	requireNotice(t, campaignRoot, neverSynced)
}

func TestNeverSyncedStaysWithOnlyThisMachinesManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a")))

	requireNotice(t, campaignRoot, neverSynced)
}

func TestNeverSyncedClearsWhenAnotherMachineHoldsEveryFile(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a"), file("b.bin", 2, "h-b")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-a"), file("b.bin", 2, "h-b")))

	requireNoNotice(t, campaignRoot)
}

// The reviewer's case: another machine's manifest is populated, but with a
// different file. Nothing here has a second copy.
func TestNeverSyncedStaysWhenAnotherMachineHoldsDifferentFiles(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a")))
	commitManifest(t, campaignRoot, "studio", record(file("b.bin", 1, "h-b")))

	requireNotice(t, campaignRoot, neverSynced)
}

// A root holding git-tracked notes beside its media: every machine that pulls
// git has the notes, and its manifest job records them. The media still exist
// here only, and the notice says how many.
func TestNeverSyncedCountsFilesAnotherMachineLacks(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(
		file("notes.md", 10, "h-notes"),
		file("cut-30s.mp4", 4000, "h-30"),
		file("cut.mp4", 9000, "h-full"),
	))
	commitManifest(t, campaignRoot, "studio", record(file("notes.md", 10, "h-notes")))

	requireNotice(t, campaignRoot, "2 of 3 files under "+testRoot+" exist on this machine only")
}

func TestNeverSyncedSaysExistsForOneUncoveredFile(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a"), file("b.bin", 2, "h-b")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-a")))

	requireNotice(t, campaignRoot, "1 of 2 files under "+testRoot+" exists on this machine only")
}

func TestNeverSyncedStaysOnAHashMismatch(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-local")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-other")))

	requireNotice(t, campaignRoot, neverSynced)
}

func TestNeverSyncedStaysOnASizeMismatch(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 2, "")))

	requireNotice(t, campaignRoot, neverSynced)
}

// A file written while it was hashed is recorded with no hash, so path and
// size are all there is to compare.
func TestNeverSyncedMatchesPathAndSizeWhenAHashIsUnknown(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-a")))

	requireNoNotice(t, campaignRoot)
}

// A machine without the root still commits a manifest for it, with no files.
func TestNeverSyncedIgnoresAnotherMachinesEmptyManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a")))
	commitManifest(t, campaignRoot, "studio", record())

	requireNotice(t, campaignRoot, neverSynced)
}

// A manifest whose embedded root is another root's is not this root's record.
func TestNeverSyncedIgnoresAManifestForAnotherRoot(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "")))
	path := filepath.Join(campaignRoot, filepath.FromSlash(artifacts.CommittedManifestRelPath("studio", testRoot)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"root":"elsewhere","describes_commit":"x","files":[{"path":"a.bin","size":1,"mtime_unix_nano":1}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	requireNotice(t, campaignRoot, neverSynced)
}

func TestNeverSyncedClearsWhenAPeerSnapshotHoldsEveryFile(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a")))
	saveSnapshot(t, campaignRoot, "laptop", record(file("a.bin", 1, "")))

	requireNoNotice(t, campaignRoot)
}

// Pulling from a peer whose root is empty records a snapshot with no agreed
// files; that is not a second copy.
func TestNeverSyncedIgnoresAnEmptySnapshot(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a")))
	saveSnapshot(t, campaignRoot, "laptop", record())

	requireNotice(t, campaignRoot, neverSynced)
}

// Coverage is per file across every record held elsewhere, so two partial
// copies on different machines together clear the notice.
func TestNeverSyncedCombinesManifestsAndSnapshots(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("a.bin", 1, "h-a"), file("b.bin", 2, "h-b")))
	commitManifest(t, campaignRoot, "studio", record(file("a.bin", 1, "h-a")))
	saveSnapshot(t, campaignRoot, "laptop", record(file("b.bin", 2, "")))

	requireNoNotice(t, campaignRoot)
}

// A dismissal committed by camp v0.10.0, under the long id, keeps working.
func TestNeverSyncedHonorsAV010Dismissal(t *testing.T) {
	campaignRoot := stageRoot(t)
	legacy := "version: 1\ndismissed:\n    artifact-root-never-synced:" + testRoot + ": 2026-09-01T00:00:00Z\n"
	if err := os.WriteFile(DismissalPath(campaignRoot), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	requireNoNotice(t, campaignRoot)
	data, err := os.ReadFile(DismissalPath(campaignRoot))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != legacy {
		t.Errorf("reading dismissals rewrote the committed file:\n%s", data)
	}
}

func TestManifestDriftUsesAShortID(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record(file("gone.bin", 1, "")))

	n, err := ArtifactRootDrift(context.Background(), campaignRoot)
	if err != nil {
		t.Fatal(err)
	}
	if n == nil {
		t.Fatal("a root that no longer matches its record must notify")
	}
	if want := SubjectID(KindManifestDrift, testRoot); n.ID != want {
		t.Errorf("ID = %q, want %q", n.ID, want)
	}
	if n.Subject != testRoot {
		t.Errorf("Subject = %q, want %q", n.Subject, testRoot)
	}
	if !strings.Contains(n.Command, "camp notify dismiss "+n.ID) {
		t.Errorf("Command %q does not carry its own dismiss id", n.Command)
	}

	legacy := "version: 1\ndismissed:\n    artifact-manifest-drift:" + testRoot + ": 2026-09-01T00:00:00Z\n"
	if err := os.WriteFile(DismissalPath(campaignRoot), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err = ArtifactRootDrift(context.Background(), campaignRoot); err != nil || n != nil {
		t.Fatalf("a v0.10.0 drift dismissal must still silence the notice; got %+v, %v", n, err)
	}
}
