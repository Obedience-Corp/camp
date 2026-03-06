package dungeon

import (
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// CrawlIgnoreFile is the name of the crawlignore file placed in a directory
// to exclude items from the dungeon triage crawl using gitignore-style patterns.
const CrawlIgnoreFile = ".crawlignore"

// CrawlIgnoreMatcher wraps patternmatcher to provide gitignore-style
// exclusion matching for crawl items.
type CrawlIgnoreMatcher struct {
	pm *patternmatcher.PatternMatcher
}

// LoadCrawlIgnore reads a .crawlignore file and returns a matcher.
// Returns an error wrapping os.ErrNotExist if the file is missing.
func LoadCrawlIgnore(path string) (*CrawlIgnoreMatcher, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, camperrors.Wrap(err, "opening crawlignore")
	}
	defer f.Close()

	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading crawlignore")
	}

	if len(patterns) == 0 {
		return &CrawlIgnoreMatcher{}, nil
	}

	pm, err := patternmatcher.New(patterns)
	if err != nil {
		return nil, camperrors.Wrap(err, "compiling crawlignore patterns")
	}

	return &CrawlIgnoreMatcher{pm: pm}, nil
}

// Excludes reports whether the given entry name should be excluded.
// Returns false if the matcher has no patterns.
func (m *CrawlIgnoreMatcher) Excludes(name string, isDir bool) (bool, error) {
	if m.pm == nil {
		return false, nil
	}

	// patternmatcher expects slash-delimited paths. For directory-only
	// patterns (trailing /) to work, we must not append a slash ourselves —
	// patternmatcher handles directories via parent-path matching when the
	// pattern ends with a separator. Since we match single-level names,
	// MatchesOrParentMatches is equivalent to Matches here.
	return m.pm.MatchesOrParentMatches(name)
}
