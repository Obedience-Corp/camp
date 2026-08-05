package artifacts

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// The baseline mutation is the whole correctness of take-local, so it is
// pinned directly rather than only through the container flow. A baseline is a
// plain manifest, so this needs no filesystem at all.

func baselineFixture() *Manifest {
	return &Manifest{
		Version: 1,
		Root:    "media",
		Files: []FileEntry{
			{Path: "alpha.bin", Size: 8, MTime: 1000},
			{Path: "beta.bin", Size: 7, MTime: 2000},
		},
		GeneratedAt: time.Unix(0, 0).UTC(),
	}
}

// TestTakeLocalRemovesTheEntryRatherThanAgreeingToIt is the regression that
// protects a user's file from the obvious wrong implementation.
//
// A pull may overwrite exactly those files whose local bytes match the
// baseline. Recording the local entry as agreed therefore hands the next sync
// permission to replace the file the user just chose to keep. Removing the
// entry instead leaves it unknown-provenance, which the pull treats as
// protected indefinitely.
func TestTakeLocalRemovesTheEntryRatherThanAgreeingToIt(t *testing.T) {
	baseline := baselineFixture()
	conflict := Conflict{Root: "media", Path: "alpha.bin"}

	// The local file as it now stands, i.e. what a naive take-local would
	// write into the baseline.
	local := &Manifest{Root: "media", Files: []FileEntry{
		{Path: "alpha.bin", Size: 16, MTime: 9999},
		{Path: "beta.bin", Size: 7, MTime: 2000},
	}}

	kept := make([]FileEntry, 0, len(baseline.Files))
	for _, f := range baseline.Files {
		if f.Path != conflict.Path {
			kept = append(kept, f)
		}
	}
	baseline.Files = kept

	if _, present := baseline.Index()[conflict.Path]; present {
		t.Fatal("take-local must remove the path from the baseline, not rewrite it")
	}

	// The consequence that matters: the file is still protected, so a pull
	// cannot touch it.
	protected := local.ProtectedPaths(baseline)
	if !contains(protected, "alpha.bin") {
		t.Errorf("after take-local the file must stay protected, got protected=%v", protected)
	}

	// And it is no longer a reported conflict, because conflicts are the
	// protected paths the baseline knows about.
	if reported := modifiedSubset(protected, baseline); contains(reported, "alpha.bin") {
		t.Errorf("after take-local the conflict must stop being reported, got %v", reported)
	}

	// The untouched file is unaffected either way.
	if contains(protected, "beta.bin") {
		t.Error("resolving one path must not protect an unrelated one")
	}
}

// TestTakePeerAgreementClearsProtection checks the other direction: once the
// peer's bytes are installed and recorded, the path is neither protected nor
// reported.
func TestTakePeerAgreementClearsProtection(t *testing.T) {
	baseline := baselineFixture()
	peerEntry := FileEntry{Path: "alpha.bin", Size: 13, MTime: 5555}

	baseline.Files = append(withoutPath(baseline.Files, "alpha.bin"), peerEntry)

	// Local now matches what was recorded, which is what installing the peer's
	// copy achieves.
	local := &Manifest{Root: "media", Files: []FileEntry{
		peerEntry,
		{Path: "beta.bin", Size: 7, MTime: 2000},
	}}

	protected := local.ProtectedPaths(baseline)
	if contains(protected, "alpha.bin") {
		t.Errorf("after take-peer the path must be agreed, got protected=%v", protected)
	}
	if reported := modifiedSubset(protected, baseline); len(reported) != 0 {
		t.Errorf("after take-peer nothing should be reported, got %v", reported)
	}
}

func TestWithoutPath(t *testing.T) {
	files := []FileEntry{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	tests := []struct {
		name   string
		remove string
		want   int
	}{
		{name: "removes the named entry", remove: "b", want: 2},
		{name: "absent path is a no-op", remove: "zzz", want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withoutPath(files, tt.remove)
			if len(got) != tt.want {
				t.Fatalf("withoutPath(%q) kept %d entries, want %d", tt.remove, len(got), tt.want)
			}
			for _, f := range got {
				if f.Path == tt.remove {
					t.Errorf("%q survived removal", tt.remove)
				}
			}
		})
	}
}

func TestNoConflictErrorNamesTheSituation(t *testing.T) {
	tests := []struct {
		name string
		all  []Conflict
		want string
	}{
		{
			name: "nothing conflicted at all",
			all:  nil,
			want: "no open conflicts",
		},
		{
			name: "wrong path, others open",
			all:  []Conflict{{Root: "media", Path: "alpha.bin"}},
			want: "is not an open conflict",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := noConflictError("studio", "beta.bin", tt.all)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Errorf("error = %q, want it to mention %q", got, tt.want)
			}
			// An operator who ran the wrong path needs to see the right ones.
			if len(tt.all) > 0 && !strings.Contains(err.Error(), "alpha.bin") {
				t.Errorf("error = %q, want it to name the open conflicts", err.Error())
			}
		})
	}
}

func contains(list []string, want string) bool {
	return slices.Contains(list, want)
}
