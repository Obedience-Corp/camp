package crawl

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildCommitPaths_IncludesAuditAndCrawlLog(t *testing.T) {
	cp := BuildCommitPaths(
		[]string{".campaign/intents/ready/a.md"},
		[]string{".campaign/intents/inbox/a.md"},
		".campaign/intents",
	)

	if len(cp.Files) == 0 {
		t.Fatal("Files empty")
	}
	if !containsString(cp.Files, ".campaign/intents/ready/a.md") {
		t.Errorf("destination missing: %v", cp.Files)
	}
	if !containsString(cp.Files, ".campaign/intents/.intents.jsonl") {
		t.Errorf("audit log missing: %v", cp.Files)
	}
	if !containsString(cp.Files, ".campaign/intents/crawl.jsonl") {
		t.Errorf("crawl log missing: %v", cp.Files)
	}
	if !containsString(cp.PreStaged, ".campaign/intents/inbox/a.md") {
		t.Errorf("pre-staged source missing: %v", cp.PreStaged)
	}
}

func TestBuildCommitPaths_DropsInvalidPathsButKeepsAbsolute(t *testing.T) {
	// Absolute paths are valid production input from IntentService;
	// the cobra command normalises them via commit.NormalizeFiles
	// before the commit. Only definitively invalid paths (empty,
	// ".", "..", relative escapes) are dropped here.
	cp := BuildCommitPaths(
		[]string{
			"",
			".",
			"..",
			"/abs/intents/ready/abs.md",
			"../escape.md",
			".campaign/intents/ready/safe.md",
		},
		nil,
		".campaign/intents",
	)
	if !containsString(cp.Files, ".campaign/intents/ready/safe.md") {
		t.Errorf("expected relative path retained: %v", cp.Files)
	}
	if !containsString(cp.Files, "/abs/intents/ready/abs.md") {
		t.Errorf("expected absolute path retained for downstream normalisation: %v", cp.Files)
	}
	for _, p := range cp.Files {
		if strings.HasPrefix(p, "..") {
			t.Errorf("relative escape leaked: %q", p)
		}
	}
}

func TestBuildCommitPaths_DedupesDestinations(t *testing.T) {
	cp := BuildCommitPaths(
		[]string{
			".campaign/intents/ready/a.md",
			".campaign/intents/ready/a.md",
		},
		nil,
		".campaign/intents",
	)
	count := 0
	for _, f := range cp.Files {
		if f == ".campaign/intents/ready/a.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("destination duplicated %d times in commit paths", count)
	}
}

func TestBuildCommitPaths_SortsOutput(t *testing.T) {
	cp := BuildCommitPaths(
		[]string{
			".campaign/intents/dungeon/done/z.md",
			".campaign/intents/ready/a.md",
		},
		nil,
		".campaign/intents",
	)
	for i := 1; i < len(cp.Files); i++ {
		if cp.Files[i-1] > cp.Files[i] {
			t.Errorf("Files not sorted at %d: %v", i, cp.Files)
		}
	}
}

func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
