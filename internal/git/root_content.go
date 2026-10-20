package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// RootPlan is what a stage-everything commit does with untracked content
// inside declared artifact roots.
//
// Size decides, as it does everywhere else, under the large-file guard's
// mode. A new file under the threshold, one commit.guards.allow names, or one
// git LFS manages belongs in the commit: "untracked content in a root is
// never staged" would silently drop a note written beside the footage.
// Larger files stay out and are tallied. A nested repository stays out
// whole. A directory link is committed as its link, never as the files in
// its target.
//
// A clean root is gitignored, so nothing in it is listed and nothing is
// walked.
type RootPlan struct {
	Commit   []stageguard.Candidate
	Kept     []stageguard.Candidate
	KeptDirs []string
	// Links are directory links inside the roots. They are staged as link
	// blobs, the way the rest of the stage handles them.
	Links []string
	// Moved are kept files that were tracked under another path. The tally
	// leaves them out; they are reported one by one.
	Moved []RootMove
}

// RootTally is one root's kept-out content, for the "kept out of git" line.
type RootTally struct {
	Root  string
	Files int
	Bytes int64
	// NestedRepos are embedded repositories inside the root, kept out whole.
	NestedRepos []string
}

// CommitPaths returns the repo-relative paths the plan commits.
func (p RootPlan) CommitPaths() []string {
	paths := make([]string, 0, len(p.Commit))
	for _, c := range p.Commit {
		paths = append(paths, c.Path)
	}
	return paths
}

// PlanRootContent sizes every untracked file in roots and decides which
// belong in the commit.
//
// The large-file mode decides as it does for camp status: auto keeps
// over-threshold files out, off keeps nothing out, and under block the
// caller refuses a commit that would keep anything out. admitLarge is
// --commit-large, which commits the over-threshold files. A nil limits,
// meaning the policy could not be read, keeps everything out.
func PlanRootContent(ctx context.Context, repoPath string, roots []string, limits *stageguard.GuardLimits, admitLarge bool) (RootPlan, error) {
	if ctx.Err() != nil {
		return RootPlan{}, ctx.Err()
	}
	if len(roots) == 0 {
		return RootPlan{}, nil
	}
	args := []string{"ls-files", "-z", "-o", "--exclude-standard", "--"}
	for _, root := range roots {
		args = append(args, ":(literal)"+strings.TrimSuffix(root, "/"))
	}
	out, err := Output(ctx, repoPath, args...)
	if err != nil {
		return RootPlan{}, camperrors.Wrap(err, "list untracked files in artifact roots")
	}

	var plan RootPlan
	var over []stageguard.Candidate
	maxSize := stageguard.DefaultMaxFileSize
	if limits != nil && limits.MaxFileSize > 0 {
		maxSize = limits.MaxFileSize
	}
	keepNone := limits != nil && (admitLarge || limits.LargeFiles == stageguard.ModeOff)
	links := newLinkFinder(repoPath)
	seenLink := map[string]bool{}
	for _, entry := range splitNull(out) {
		path := strings.TrimSuffix(entry, "/")
		link, linkErr := links.ancestor(path)
		if linkErr != nil {
			return RootPlan{}, linkErr
		}
		if link != "" {
			if !seenLink[link] {
				seenLink[link] = true
				plan.Links = append(plan.Links, link)
			}
			continue
		}
		if strings.HasSuffix(entry, "/") {
			plan.KeptDirs = append(plan.KeptDirs, path)
			continue
		}
		info, statErr := os.Lstat(filepath.Join(repoPath, filepath.FromSlash(path)))
		if statErr != nil || info.IsDir() {
			continue
		}
		candidate := stageguard.Candidate{Path: path, Size: info.Size(), Untracked: true}
		switch {
		case limits == nil:
			plan.Kept = append(plan.Kept, candidate)
		case keepNone, stageguard.MatchesAllow(path, limits.Allow), candidate.Size <= maxSize:
			plan.Commit = append(plan.Commit, candidate)
		default:
			over = append(over, candidate)
		}
	}
	if len(over) == 0 {
		return plan, nil
	}
	notLFS, err := stageguard.FilterLFSManaged(ctx, repoPath, over)
	if err != nil {
		return RootPlan{}, camperrors.Wrap(err, "check which artifact root files git LFS manages")
	}
	kept := make(map[string]bool, len(notLFS))
	for _, c := range notLFS {
		kept[c.Path] = true
	}
	for _, c := range over {
		if kept[c.Path] {
			plan.Kept = append(plan.Kept, c)
		} else {
			plan.Commit = append(plan.Commit, c)
		}
	}
	return plan, nil
}

// KeptViolations describes the kept-out files the way the staging guard
// describes an over-threshold file, for a block-mode refusal.
func (p RootPlan) KeptViolations() []stageguard.GuardViolation {
	violations := make([]stageguard.GuardViolation, 0, len(p.Kept))
	for _, c := range p.Kept {
		violations = append(violations, stageguard.GuardViolation{
			Kind: stageguard.OverThreshold,
			Path: c.Path,
			Size: c.Size,
		})
	}
	return violations
}

// linkFinder finds the directory link a listed path sits beneath, checking
// each parent directory once. Git for Windows can list the files inside a
// junction's target as if they were the camp's own.
type linkFinder struct {
	repoPath string
	dirs     map[string]bool
}

func newLinkFinder(repoPath string) *linkFinder {
	return &linkFinder{repoPath: repoPath, dirs: map[string]bool{}}
}

func (f *linkFinder) ancestor(path string) (string, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		isLink, known := f.dirs[prefix]
		if !known {
			stat, err := dirlink.Lstat(filepath.Join(f.repoPath, filepath.FromSlash(prefix)))
			if err != nil {
				if camperrors.Is(err, os.ErrNotExist) {
					return "", nil
				}
				return "", camperrors.Wrapf(err, "classify %s", prefix)
			}
			isLink = stat.IsDirectoryLink()
			if i < len(parts)-1 {
				f.dirs[prefix] = isLink
			}
		}
		if isLink {
			return prefix, nil
		}
	}
	return "", nil
}

// Tally groups the kept-out content by root, in root order.
func (p RootPlan) Tally(roots []string) []RootTally {
	byRoot := make(map[string]*RootTally)
	var order []string
	entry := func(path string) *RootTally {
		root := rootOf(path, roots)
		if root == "" {
			return nil
		}
		if t, ok := byRoot[root]; ok {
			return t
		}
		t := &RootTally{Root: root}
		byRoot[root] = t
		order = append(order, root)
		return t
	}
	moved := make(map[string]bool, len(p.Moved))
	for _, m := range p.Moved {
		moved[m.To] = true
	}
	for _, c := range p.Kept {
		if moved[c.Path] {
			continue
		}
		if t := entry(c.Path); t != nil {
			t.Files++
			t.Bytes += c.Size
		}
	}
	for _, dir := range p.KeptDirs {
		if t := entry(dir); t != nil {
			t.NestedRepos = append(t.NestedRepos, dir)
		}
	}
	tallies := make([]RootTally, 0, len(order))
	for _, root := range order {
		tallies = append(tallies, *byRoot[root])
	}
	return tallies
}

func rootOf(path string, roots []string) string {
	for _, root := range roots {
		root = strings.TrimSuffix(root, "/")
		if path == root || strings.HasPrefix(path, root+"/") {
			return root
		}
	}
	return ""
}

// RootMove is a tracked file renamed inside a declared root, or moved into
// one, whose new path is over the threshold. Size keeps the new path out of
// git with the root's media, and git records the old path as deleted. That
// is said out loud on its own line, never folded into the media tally.
type RootMove struct {
	From string
	To   string
	Size int64
}

// PairMovesIntoRoots finds which kept-out root files git would pair with a
// deleted tracked path as a rename.
//
// deleted are tracked paths missing from the worktree. newFiles are the
// untracked files this commit adds; they compete for the same deletions, so
// both commit paths pair the same way whether or not they are staged yet.
//
// Nothing is hashed unless a kept file is within git's size bound of a
// deleted blob, which needs a tracked file of comparable size to have been
// deleted. Detection runs in a copy of the index; the real one is untouched.
func PairMovesIntoRoots(ctx context.Context, repoPath string, kept []stageguard.Candidate, deleted, newFiles []string) ([]RootMove, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(kept) == 0 || len(deleted) == 0 {
		return nil, nil
	}
	sizes, err := headBlobSizes(ctx, repoPath, deleted)
	if err != nil || len(sizes) == 0 {
		return nil, err
	}
	sorted := make([]int64, 0, len(sizes))
	for _, size := range sizes {
		sorted = append(sorted, size)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	candidates := map[string]int64{}
	for _, c := range kept {
		if renameSizeCompatible(c.Size, sorted) {
			candidates[c.Path] = c.Size
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	tmp, real, err := BuildTempIndexPath(repoPath)
	if err != nil {
		return nil, err
	}
	defer RemoveTempIndex(tmp)
	if _, statErr := os.Stat(real); statErr == nil {
		if err := CopyFile(real, tmp); err != nil {
			return nil, err
		}
	} else if err := ReadTreeIntoTempIndex(ctx, repoPath, tmp); err != nil {
		return nil, err
	}
	intent := append([]string(nil), newFiles...)
	for path := range candidates {
		intent = append(intent, path)
	}
	sort.Strings(intent)
	for _, batch := range splitPathspecsForExec(intent, resetPathspecPayloadLimit(repoPath)) {
		args := []string{"add", "--intent-to-add", "--"}
		for _, path := range batch {
			args = append(args, ":(literal)"+path)
		}
		if _, err := runWithIndex(ctx, repoPath, tmp, args...); err != nil {
			return nil, camperrors.Wrap(err, "mark candidate paths for rename detection")
		}
	}
	out, err := runWithIndex(ctx, repoPath, tmp,
		"diff", "-z", "--name-status", "--no-color", "--no-ext-diff", "--find-renames", "--diff-filter=R", "HEAD")
	if err != nil {
		return nil, camperrors.Wrap(err, "detect renames into artifact roots")
	}
	var moves []RootMove
	for _, pair := range renamePairs(out) {
		size, ok := candidates[pair[1]]
		if _, wasTracked := sizes[pair[0]]; ok && wasTracked {
			moves = append(moves, RootMove{From: pair[0], To: pair[1], Size: size})
		}
	}
	return moves, nil
}

// StagedDeletions lists tracked paths the index records as deleted against
// HEAD. It reads the index only, so it does not walk the worktree.
func StagedDeletions(ctx context.Context, repoPath string) ([]string, error) {
	if !headResolvable(ctx, repoPath) {
		return nil, nil
	}
	out, err := Output(ctx, repoPath, "diff", "--cached", "-z", "--name-only", "--no-renames", "--diff-filter=D", "HEAD")
	if err != nil {
		return nil, camperrors.Wrap(err, "list staged deletions")
	}
	return splitNull(out), nil
}

// headBlobSizes returns the HEAD blob size of each path. A path git cannot
// name on one line, or one that is not a blob, has no entry.
func headBlobSizes(ctx context.Context, repoPath string, paths []string) (map[string]int64, error) {
	var named []string
	var input strings.Builder
	for _, path := range paths {
		if strings.ContainsAny(path, "\n\r") {
			continue
		}
		named = append(named, path)
		input.WriteString("HEAD:" + path + "\n")
	}
	if len(named) == 0 {
		return nil, nil
	}
	cmd := gitCmd(ctx, repoPath, "cat-file", "--batch-check=%(objecttype) %(objectsize)")
	cmd.Stdin = strings.NewReader(input.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "read deleted blob sizes: %s", strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	sizes := make(map[string]int64, len(named))
	for i, line := range lines {
		if i >= len(named) {
			break
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "blob" {
			continue
		}
		if size, parseErr := strconv.ParseInt(fields[1], 10, 64); parseErr == nil {
			sizes[named[i]] = size
		}
	}
	return sizes, nil
}

// renameSizeCompatible reports whether a file of size could reach git's
// default 50% rename score against one of the sorted sizes. Git refuses a
// pair before reading either side when the larger is more than twice the
// smaller, so a candidate outside that bound is never worth hashing.
func renameSizeCompatible(size int64, sorted []int64) bool {
	low := (size + 1) / 2
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i] >= low })
	return i < len(sorted) && sorted[i] <= 2*size
}

// renamePairs reads source and destination from `git diff -z --name-status`
// rename records.
func renamePairs(out []byte) [][2]string {
	fields := strings.Split(string(out), "\x00")
	var pairs [][2]string
	for i := 0; i+2 < len(fields); {
		status := fields[i]
		if strings.HasPrefix(status, "R") {
			pairs = append(pairs, [2]string{fields[i+1], fields[i+2]})
			i += 3
			continue
		}
		i += 2
	}
	return pairs
}

func runWithIndex(ctx context.Context, repoPath, indexPath string, args ...string) ([]byte, error) {
	cmd := gitCmd(ctx, repoPath, args...)
	cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+indexPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
