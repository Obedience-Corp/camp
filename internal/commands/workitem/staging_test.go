package workitem

import (
	"reflect"
	"testing"

	campgit "github.com/Obedience-Corp/camp/internal/git"
)

func TestParseGitStatusPorcelainZ_RenameKeepsNewPath(t *testing.T) {
	out := []byte("R  new name\x00old name\x00?? path with \"quote\".md\x00")
	entries := campgit.ParseStatusPorcelainZ(out)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want 2", entries)
	}
	if entries[0].Path != "new name" {
		t.Fatalf("rename path = %q, want new name", entries[0].Path)
	}
	if entries[1].Path != "path with \"quote\".md" {
		t.Fatalf("quoted path = %q", entries[1].Path)
	}
}

func TestParseDiffNameStatusZ_RenameIncludesBothPaths(t *testing.T) {
	out := []byte("R100\x00a.txt\x00b.txt\x00")
	paths, err := campgit.ParseDiffNameStatusZ(out)
	if err != nil {
		t.Fatalf("ParseDiffNameStatusZ() error = %v", err)
	}
	if !contains(paths, "a.txt") || !contains(paths, "b.txt") {
		t.Fatalf("ParseDiffNameStatusZ() = %v, want both rename paths", paths)
	}
}

func TestApplyExcludesDoesNotMutateInput(t *testing.T) {
	stage := []string{"a.md", "b.md", "c.md"}
	original := append([]string{}, stage...)
	var skip []SkippedEntry

	got := applyExcludes(stage, []string{"b.md"}, &skip)
	if !reflect.DeepEqual(stage, original) {
		t.Fatalf("applyExcludes mutated input: got %v, want %v", stage, original)
	}
	if contains(got, "b.md") {
		t.Fatalf("excluded path still present: %v", got)
	}
	if !skipContains(skip, "b.md", skipReasonExcludeFlag) {
		t.Fatalf("excluded path missing from skip list: %#v", skip)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func skipContains(haystack []SkippedEntry, path, reason string) bool {
	for _, e := range haystack {
		if e.Path == path && e.Reason == reason {
			return true
		}
	}
	return false
}
