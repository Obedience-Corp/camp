package stageguard

import (
	"context"
	"path"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// CheckStaging enumerates what a stage-everything operation would add in
// repoPath and returns the violations found under limits. It is stat-only: no
// file is opened, read, or hashed.
//
// It is exported separately from staging so a caller that stages by another
// route, or wants to preflight without staging, can still consult the guard.
func CheckStaging(ctx context.Context, repoPath string, limits GuardLimits) ([]GuardViolation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	candidates, err := Enumerate(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	candidates, err = FilterLFSManaged(ctx, repoPath, candidates)
	if err != nil {
		return nil, err
	}
	candidates = filterAllowed(candidates, limits.Allow)

	// The per-file guard runs first and the bulk guard counts only what it
	// leaves behind. Otherwise one auto-handled 512 MB video would also swell
	// the bulk batch, turning the flagship scenario into a block.
	violations, remaining := checkPerFile(candidates, limits)
	if bulk, found := detectBulk(remaining, limits); found {
		violations = append(violations, bulk)
	}
	return violations, nil
}

// checkPerFile applies the size threshold and returns both the violations and
// the candidates the bulk guard should still consider.
func checkPerFile(candidates []Candidate, limits GuardLimits) (violations []GuardViolation, remaining []Candidate) {
	remaining = make([]Candidate, 0, len(candidates))
	if limits.LargeFiles == ModeOff {
		return nil, append(remaining, candidates...)
	}

	for _, candidate := range candidates {
		// Size decides, never extension: a 200 KB .mp4 is ordinary content and
		// a 512 MB .json is not.
		if candidate.Size <= limits.MaxFileSize {
			remaining = append(remaining, candidate)
			continue
		}
		if !candidate.Untracked {
			// A tracked file that grew past the threshold is reported and
			// committed. Excluding it would leave the stale blob in git while
			// the new bytes on disk belonged to nothing.
			violations = append(violations, GuardViolation{
				Kind: TrackedGrowth,
				Path: candidate.Path,
				Size: candidate.Size,
			})
			remaining = append(remaining, candidate)
			continue
		}
		violations = append(violations, GuardViolation{
			Kind: OverThreshold,
			Path: candidate.Path,
			Size: candidate.Size,
		})
	}
	return violations, remaining
}

// detectBulk reports one staging operation adding a very large number of
// untracked files under a single directory. Tracked modifications never count:
// editing a thousand existing files is ordinary work, and the pathology being
// caught is a vendor tree arriving whole.
func detectBulk(candidates []Candidate, limits GuardLimits) (GuardViolation, bool) {
	if limits.Bulk == ModeOff || limits.MaxAddedFiles <= 0 {
		return GuardViolation{}, false
	}

	untracked := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Untracked {
			untracked = append(untracked, candidate)
		}
	}
	if len(untracked) < limits.MaxAddedFiles {
		return GuardViolation{}, false
	}

	prefix, count, total := heaviestPrefix(untracked, limits.MaxAddedFiles)
	if prefix == "" {
		return GuardViolation{}, false
	}
	return GuardViolation{
		Kind:         Bulk,
		CommonPrefix: prefix,
		Count:        count,
		// Reported for display only. There is deliberately no total-bytes
		// trigger: its only unique catch was many medium files, which is
		// exactly the false positive.
		TotalBytes: total,
	}, true
}

// heaviestPrefix finds the deepest directory holding at least minCount of the
// candidates, and returns it with its file count and total size. It returns an
// empty prefix when no single directory holds that many, which is the ordinary
// scattered batch that must not block.
//
// The test is an absolute count, not a share of the batch. A share can be
// diluted: 1000 files under vendor/ plus 120 unrelated stragglers is 89% of the
// batch, so a 90% rule cancels a vendor dump that is exactly what the bulk
// guard exists to catch. Whether a directory is being dumped into git does not
// depend on what else the user happened to leave lying around.
//
// The descent is kept because it names the reported path: reporting "vendor"
// when the files are all under "vendor/github.com/pkg" tells the user less
// about what they are looking at.
func heaviestPrefix(candidates []Candidate, minCount int) (prefix string, count int, total int64) {
	current := ""

	for depth := 1; ; depth++ {
		counts := make(map[string]int)
		for _, candidate := range candidates {
			dir, ok := prefixAtDepth(candidate.Path, depth)
			if !ok || !underPrefix(candidate.Path, current) {
				continue
			}
			counts[dir]++
		}

		best, bestCount := "", 0
		for dir, n := range counts {
			// Ties resolve by name so the result never depends on map order.
			if n > bestCount || (n == bestCount && dir < best) {
				best, bestCount = dir, n
			}
		}
		if bestCount < minCount || best == "" {
			break
		}
		current = best
	}

	if current == "" {
		return "", 0, 0
	}
	for _, candidate := range candidates {
		if underPrefix(candidate.Path, current) {
			count++
			total += candidate.Size
		}
	}
	return current, count, total
}

// prefixAtDepth returns the first depth segments of a path's directory, or
// false when the path is not that deep. A file at the repo root has no
// directory prefix and can never be part of a dominant one.
func prefixAtDepth(p string, depth int) (string, bool) {
	segments := strings.Split(p, "/")
	if len(segments) <= depth {
		return "", false
	}
	return strings.Join(segments[:depth], "/"), true
}

// underPrefix reports whether p lives beneath prefix. An empty prefix matches
// everything, which is how the descent starts.
func underPrefix(p, prefix string) bool {
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(p, prefix+"/")
}

// filterAllowed drops candidates matched by an allowlist glob. The allowlist
// is the one obvious line that makes a legitimate large file permanent, so it
// is applied before any guard rather than as an exception inside one.
func filterAllowed(candidates []Candidate, allow []string) []Candidate {
	if len(allow) == 0 {
		return candidates
	}
	kept := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if MatchesAllow(candidate.Path, allow) {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

// MatchesAllow reports whether a repo-relative path matches any allow glob.
//
// Three forms are supported, chosen to match what users already expect from
// gitignore rather than inventing a syntax:
//
//   - an exact path
//   - "dir/**", matching everything beneath dir
//   - a glob; one without a slash matches the basename at any depth, so
//     "*.psd" is not accidentally anchored to the repository root
func MatchesAllow(p string, allow []string) bool {
	for _, pattern := range allow {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchesOne(p, pattern) {
			return true
		}
	}
	return false
}

func matchesOne(p, pattern string) bool {
	if p == pattern {
		return true
	}
	if rest, ok := strings.CutSuffix(pattern, "/**"); ok {
		return p == rest || strings.HasPrefix(p, rest+"/")
	}
	if ok, err := path.Match(pattern, p); err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		if ok, err := path.Match(pattern, path.Base(p)); err == nil && ok {
			return true
		}
	}
	return false
}

// ValidateAllow reports the first malformed glob in an allowlist.
//
// Matching deliberately ignores pattern errors so one bad entry cannot break
// evaluation of the rest, which means a typo would otherwise be invisible: the
// user believes a path is exempt, the guard silently disagrees, and the file
// gets excluded from their commit anyway. Config resolution calls this so the
// mistake surfaces where it can be fixed.
func ValidateAllow(allow []string) error {
	for _, pattern := range allow {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		probe := strings.TrimSuffix(trimmed, "/**")
		if _, err := path.Match(probe, "probe"); err != nil {
			return camperrors.Wrapf(err, "invalid allow glob %q", pattern)
		}
	}
	return nil
}

// SortViolations orders violations for stable reporting: bulk first because it
// blocks, then per-file findings by descending size, then by path.
func SortViolations(violations []GuardViolation) {
	sort.SliceStable(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if (a.Kind == Bulk) != (b.Kind == Bulk) {
			return a.Kind == Bulk
		}
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		return a.Path < b.Path
	})
}
