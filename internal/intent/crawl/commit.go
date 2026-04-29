package crawl

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

// commitPathSet collects unique, safe relative paths for a batch
// commit. It drops absolute, parent-traversing, and empty paths so
// the commit helper never sees an unsafe target.
type commitPathSet struct {
	seen  map[string]struct{}
	paths []string
}

func newCommitPathSet() *commitPathSet {
	return &commitPathSet{seen: map[string]struct{}{}}
}

func (s *commitPathSet) add(path string) {
	clean, ok := cleanRelPath(path)
	if !ok {
		return
	}
	if _, dup := s.seen[clean]; dup {
		return
	}
	s.seen[clean] = struct{}{}
	s.paths = append(s.paths, clean)
}

func (s *commitPathSet) sorted() []string {
	sort.Strings(s.paths)
	return s.paths
}

func cleanRelPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	c := filepath.Clean(p)
	if c == "." || c == ".." || filepath.IsAbs(c) {
		return "", false
	}
	if strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", false
	}
	return c, true
}

// CommitPaths bundles the path lists needed to drive a batch
// commit at the campaign root after a crawl session.
type CommitPaths struct {
	// Files are destination paths plus the audit and crawl logs.
	// They are passed to the commit helper as the explicit file
	// scope.
	Files []string
	// PreStaged are tracked source paths that must be staged for
	// deletion before the commit helper runs (analogous to dungeon
	// crawl's stageTrackedCrawlSourceDeletions).
	PreStaged []string
}

// BuildCommitPaths assembles the file lists needed for the batch
// commit. Inputs:
//   - destinations: campaign-root-relative paths of moved intents
//     (Summary.Paths flattened and deduped).
//   - sources: campaign-root-relative paths of intent files that
//     were removed by Move (recorded by the runner before each move).
//   - intentsDir: campaign-root-relative path to the intents
//     directory (e.g., ".campaign/intents").
//
// The resulting CommitPaths always includes the audit log and
// crawl log. Empty results mean no commit is needed.
func BuildCommitPaths(destinations, sources []string, intentsDir string) CommitPaths {
	files := newCommitPathSet()
	for _, d := range destinations {
		files.add(d)
	}
	files.add(audit.FilePath(intentsDir))
	files.add(CrawlLogPath(intentsDir))

	pre := newCommitPathSet()
	for _, s := range sources {
		pre.add(s)
	}

	return CommitPaths{
		Files:     files.sorted(),
		PreStaged: pre.sorted(),
	}
}
