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

func record(files ...string) *artifacts.Manifest {
	m := &artifacts.Manifest{Version: 1, Root: testRoot}
	for _, f := range files {
		m.Files = append(m.Files, artifacts.FileEntry{Path: f, Size: 1, MTime: 1})
	}
	return m
}

func commitManifest(t *testing.T, campaignRoot, machine string, m *artifacts.Manifest) {
	t.Helper()
	if _, err := artifacts.WriteCommitted(campaignRoot, machine, m, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
}

func neverSynced(t *testing.T, campaignRoot string) *Notice {
	t.Helper()
	n, err := ArtifactRootNeverSynced(context.Background(), campaignRoot)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNeverSyncedNamesTheRemedyFromAnotherMachine(t *testing.T) {
	campaignRoot := stageRoot(t)

	n := neverSynced(t, campaignRoot)
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

func TestNeverSyncedPersistsWithOnlyThisMachinesManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record("a.bin"))

	if neverSynced(t, campaignRoot) == nil {
		t.Fatal("this machine's own manifest is not a second copy; the notice must stay")
	}
}

func TestNeverSyncedClearsWithAnotherMachinesManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, thisMachine, record("a.bin"))
	commitManifest(t, campaignRoot, "studio", record("a.bin"))

	if n := neverSynced(t, campaignRoot); n != nil {
		t.Fatalf("another machine records the root's files; got %+v", n)
	}
}

// A machine without the root still commits a manifest for it, with no files.
func TestNeverSyncedIgnoresAnotherMachinesEmptyManifest(t *testing.T) {
	campaignRoot := stageRoot(t)
	commitManifest(t, campaignRoot, "studio", record())

	if neverSynced(t, campaignRoot) == nil {
		t.Fatal("an empty manifest from another machine proves no copy; the notice must stay")
	}
}

// A manifest whose embedded root is another root's is not this root's record.
func TestNeverSyncedIgnoresAManifestForAnotherRoot(t *testing.T) {
	campaignRoot := stageRoot(t)
	path := filepath.Join(campaignRoot, filepath.FromSlash(artifacts.CommittedManifestRelPath("studio", testRoot)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"root":"elsewhere","describes_commit":"x","files":[{"path":"a.bin","size":1,"mtime_unix_nano":1}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if neverSynced(t, campaignRoot) == nil {
		t.Fatal("a mismatched record must not count as a copy of this root")
	}
}

// Pulling from a peer whose root is empty records a snapshot with no agreed
// files; that is not a second copy.
func TestNeverSyncedIgnoresAnEmptySnapshot(t *testing.T) {
	campaignRoot := stageRoot(t)
	if err := artifacts.SaveSnapshot(campaignRoot, "laptop", testRoot, record()); err != nil {
		t.Fatal(err)
	}

	if neverSynced(t, campaignRoot) == nil {
		t.Fatal("an empty snapshot must not clear the notice")
	}
}

func TestNeverSyncedClearsWithAPopulatedSnapshot(t *testing.T) {
	campaignRoot := stageRoot(t)
	if err := artifacts.SaveSnapshot(campaignRoot, "laptop", testRoot, record("a.bin")); err != nil {
		t.Fatal(err)
	}

	if n := neverSynced(t, campaignRoot); n != nil {
		t.Fatalf("a pull that agreed on files is a second copy; got %+v", n)
	}
}

// A dismissal committed by camp v0.10.0, under the long id, keeps working.
func TestNeverSyncedHonorsAV010Dismissal(t *testing.T) {
	campaignRoot := stageRoot(t)
	legacy := "version: 1\ndismissed:\n    artifact-root-never-synced:" + testRoot + ": 2026-09-01T00:00:00Z\n"
	if err := os.WriteFile(DismissalPath(campaignRoot), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if n := neverSynced(t, campaignRoot); n != nil {
		t.Fatalf("a v0.10.0 dismissal must still silence the notice; got %+v", n)
	}
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
	commitManifest(t, campaignRoot, thisMachine, record("gone.bin"))

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
