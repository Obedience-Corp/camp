package skills

import (
	"context"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"
	"time"
)

type fakeProvenance struct {
	once  map[string]bool
	asked []string
}

func (p *fakeProvenance) WrittenOnce(_ context.Context, relPath string) bool {
	p.asked = append(p.asked, relPath)
	return p.once[relPath]
}

func TestClassify_GitEvidenceDecidesUnrecordedFiles(t *testing.T) {
	shipped := []ShippedFile{
		{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")},
		{Bundle: "beta", Path: "SKILL.md", Content: []byte("beta v2\n")},
		{Bundle: "delta", Path: "SKILL.md", Content: []byte("delta v2\n")},
		{Bundle: "eps", Path: "SKILL.md", Content: []byte("eps v2\n")},
		{Bundle: "omega", Path: "SKILL.md", Content: []byte("omega v2\n")},
	}
	fsys := fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("alpha v2\n")},
		"beta/SKILL.md":  {Data: []byte("beta v1\n")},
		"delta/SKILL.md": {Data: []byte("delta edited twice\n")},
		"shared/eps.md":  {Data: []byte("eps v1\n")},
		"eps/SKILL.md":   {Data: []byte("../shared/eps.md"), Mode: fs.ModeSymlink},
	}
	prov := &fakeProvenance{once: map[string]bool{
		".campaign/skills/alpha/SKILL.md": true,
		".campaign/skills/beta/SKILL.md":  true,
		".campaign/skills/eps/SKILL.md":   true,
		".campaign/skills/omega/SKILL.md": true,
	}}

	inv, err := Classify(t.Context(), fsys, shipped, prov)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := map[string]FileStatus{
		"alpha": {State: ShippedCurrent},
		"beta":  {State: ShippedOutdated, GitEvidence: true},
		"delta": {State: ShippedUnknown},
		"eps":   {State: ShippedSymlink},
		"omega": {State: ShippedMissing},
	}
	for _, f := range inv.Files {
		w := want[f.Bundle]
		if f.State != w.State || f.GitEvidence != w.GitEvidence {
			t.Errorf("%s = %q (git %v), want %q (git %v)", f.RelPath(), f.State, f.GitEvidence, w.State, w.GitEvidence)
		}
	}
	wantAsked := []string{".campaign/skills/beta/SKILL.md", ".campaign/skills/delta/SKILL.md"}
	if !slices.Equal(prov.asked, wantAsked) {
		t.Errorf("provenance asked about %v, want only the unrecorded differing regular files %v", prov.asked, wantAsked)
	}
	if !inv.ManifestStale() {
		t.Error("a camp without a manifest must report it stale")
	}
	next := inv.NextManifest("v2", map[string]bool{".campaign/skills/beta/SKILL.md": true})
	if got, ok := next.recorded("beta", "SKILL.md"); !ok || got != HashContent([]byte("beta v2\n")) {
		t.Errorf("written git-evidence file record = %q (present %v), want the shipped hash", got, ok)
	}
	if _, ok := next.recorded("delta", "SKILL.md"); ok {
		t.Error("a preserved unknown file must stay unrecorded")
	}
}

func TestClassify_ManifestRecordOutranksGitEvidence(t *testing.T) {
	prov := &fakeProvenance{once: map[string]bool{
		".campaign/skills/beta/SKILL.md":  true,
		".campaign/skills/gamma/SKILL.md": true,
		".campaign/skills/delta/SKILL.md": true,
	}}
	inv, err := Classify(t.Context(), skillsFSFixture(), shippedFixture(), prov)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	want := map[string]FileStatus{
		".campaign/skills/beta/SKILL.md":  {State: ShippedOutdated},
		".campaign/skills/gamma/SKILL.md": {State: ShippedModified},
		".campaign/skills/delta/SKILL.md": {State: ShippedOutdated, GitEvidence: true},
	}
	for _, f := range inv.Files {
		w, ok := want[f.RelPath()]
		if ok && (f.State != w.State || f.GitEvidence != w.GitEvidence) {
			t.Errorf("%s = %q (git %v), want %q (git %v)", f.RelPath(), f.State, f.GitEvidence, w.State, w.GitEvidence)
		}
	}
	if !slices.Equal(prov.asked, []string{".campaign/skills/delta/SKILL.md"}) {
		t.Errorf("provenance asked about %v, want only delta: a manifest record is never second-guessed", prov.asked)
	}
}

func TestSecureUndo_GitEvidenceUpdatesRestoreOnlyThoseFiles(t *testing.T) {
	update := func(bundle, path string, git bool) FileOutcome {
		return FileOutcome{
			FileStatus: FileStatus{ShippedFile: ShippedFile{Bundle: bundle, Path: path}, State: ShippedOutdated, GitEvidence: git},
			Action:     ActionUpdate,
		}
	}
	files := []FileOutcome{
		update("beta", "SKILL.md", true),
		{FileStatus: FileStatus{ShippedFile: ShippedFile{Bundle: "beta", Path: "notes/new.md"}, State: ShippedMissing}, Action: ActionAdd},
		update("beta", "notes/usage.md", true),
		update("gamma", "SKILL.md", false),
	}
	undos, err := secureUndo(t.Context(), "/no/camp", "/no/camp/.campaign/skills", time.Unix(0, 0), files)
	if err != nil {
		t.Fatalf("secureUndo() error = %v", err)
	}
	want := []Undo{{Bundle: "beta", Command: "git checkout HEAD -- .campaign/skills/beta/SKILL.md .campaign/skills/beta/notes/usage.md"}}
	if !slices.Equal(undos, want) {
		t.Fatalf("undos = %+v, want %+v: only git-evidence files need an undo, and git restores them file by file", undos, want)
	}
}
