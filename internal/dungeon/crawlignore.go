package dungeon

import (
	"bufio"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/moby/patternmatcher"
)

// CrawlIgnoreFile is the name of the crawlignore file placed in a directory
// to exclude items from the dungeon triage crawl using gitignore-style patterns.
const CrawlIgnoreFile = ".crawlignore"

// CrawlIgnoreMatcher wraps patternmatcher to provide gitignore-style
// exclusion matching for crawl items. It maintains separate matchers for
// directory and file entries so that trailing-slash patterns (e.g. "build/")
// only match directories, consistent with .gitignore semantics.
type CrawlIgnoreMatcher struct {
	allPM  *patternmatcher.PatternMatcher
	filePM *patternmatcher.PatternMatcher
}

// isDirOnlyPattern reports whether a raw line targets directories only
// (trailing slash), accounting for negation prefixes.
func isDirOnlyPattern(line string) bool {
	return strings.HasSuffix(strings.TrimPrefix(line, "!"), "/")
}

// parsePatterns reads a .crawlignore file and returns two slices:
// all patterns (with trailing slashes stripped for the matcher) and
// file-only patterns (directory-only patterns excluded).
func parsePatterns(path string) (all, fileOnly []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, camperrors.Wrap(err, "opening crawlignore")
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		dirOnly := isDirOnlyPattern(line)
		// Strip trailing slash for the matcher (it doesn't understand them).
		clean := strings.TrimPrefix(line, "!")
		clean = strings.TrimSuffix(clean, "/")
		if strings.HasPrefix(line, "!") {
			clean = "!" + clean
		}

		all = append(all, clean)
		if !dirOnly {
			fileOnly = append(fileOnly, clean)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, camperrors.Wrap(err, "reading crawlignore")
	}
	return all, fileOnly, nil
}

// LoadCrawlIgnore reads a .crawlignore file and returns a matcher.
// Returns an error wrapping os.ErrNotExist if the file is missing.
func LoadCrawlIgnore(path string) (*CrawlIgnoreMatcher, error) {
	all, fileOnly, err := parsePatterns(path)
	if err != nil {
		return nil, err
	}

	if len(all) == 0 {
		return &CrawlIgnoreMatcher{}, nil
	}

	allPM, err := patternmatcher.New(all)
	if err != nil {
		return nil, camperrors.Wrap(err, "compiling crawlignore patterns")
	}

	var filePM *patternmatcher.PatternMatcher
	if len(fileOnly) > 0 {
		filePM, err = patternmatcher.New(fileOnly)
		if err != nil {
			return nil, camperrors.Wrap(err, "compiling crawlignore file patterns")
		}
	}

	return &CrawlIgnoreMatcher{allPM: allPM, filePM: filePM}, nil
}

// Excludes reports whether the given entry name should be excluded.
// Directory-only patterns (trailing slash) only match when isDir is true,
// consistent with .gitignore semantics.
func (m *CrawlIgnoreMatcher) Excludes(name string, isDir bool) (bool, error) {
	if isDir {
		if m.allPM == nil {
			return false, nil
		}
		return m.allPM.MatchesOrParentMatches(name)
	}

	if m.filePM == nil {
		return false, nil
	}
	return m.filePM.MatchesOrParentMatches(name)
}
