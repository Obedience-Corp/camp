package mdlinks

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

var (
	mdLinkRe     = regexp.MustCompile(`(!?\[(?:[^\[\]]*|\[[^\[\]]*\])*\])\(([^)]+)\)`)
	mdRefDefRe   = regexp.MustCompile(`(?m)^(\[[^\[\]]+\]:\s+)(<[^>]*>|\S+)((?:\s+"[^"]*"|\s+'[^']*'|\s+\([^)]*\))?)`)
	fencedRe     = regexp.MustCompile("(?s)```[^\n]*\n.*?```|~~~[^\n]*\n.*?~~~")
	inlineCodeRe = regexp.MustCompile("`+[^`]+`+")
)

// protectedRanges returns the byte ranges within content that are inside
// fenced code blocks or inline code spans. Links inside these ranges must
// not be rewritten.
func protectedRanges(content []byte) [][2]int {
	var ranges [][2]int
	for _, loc := range fencedRe.FindAllIndex(content, -1) {
		ranges = append(ranges, [2]int{loc[0], loc[1]})
	}
	for _, loc := range inlineCodeRe.FindAllIndex(content, -1) {
		inFence := false
		for _, r := range ranges {
			if loc[0] >= r[0] && loc[1] <= r[1] {
				inFence = true
				break
			}
		}
		if !inFence {
			ranges = append(ranges, [2]int{loc[0], loc[1]})
		}
	}
	return ranges
}

// inProtected returns true if the byte offset pos falls within any protected range.
func inProtected(pos int, ranges [][2]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

func isRelative(target string) bool {
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return false
	}
	if strings.HasPrefix(target, "/") {
		return false
	}
	if strings.HasPrefix(target, "#") {
		return false
	}
	return true
}

func splitAnchor(target string) (path, anchor string) {
	if idx := strings.LastIndex(target, "#"); idx != -1 {
		return target[:idx], target[idx:]
	}
	return target, ""
}

// stripAngleBrackets removes surrounding < > from an angle-bracket destination
// and returns the inner path and whether brackets were present.
func stripAngleBrackets(s string) (inner string, hadBrackets bool) {
	if len(s) >= 2 && s[0] == '<' && s[len(s)-1] == '>' {
		return s[1 : len(s)-1], true
	}
	return s, false
}

// rewriteTarget computes the new relative path for a link target given oldBase
// (directory the file was at before move) and newBase (directory after move).
// srcPath, when non-empty, is the moved-subtree root: targets that resolve
// inside srcPath are left unchanged because they moved with the file.
// Returns the new target string and whether a rewrite occurred.
func rewriteTarget(target, oldBase, newBase, srcPath string) (string, bool) {
	if !isRelative(target) {
		return target, false
	}

	rawPath, anchor := splitAnchor(target)
	if rawPath == "" {
		return target, false
	}

	inner, hadBrackets := stripAngleBrackets(rawPath)
	if inner == "" {
		return target, false
	}

	abs := filepath.Join(oldBase, inner)

	if srcPath != "" && pathMatchesMoved(abs, srcPath) {
		return target, false
	}

	rel, err := filepath.Rel(newBase, abs)
	if err != nil {
		return target, false
	}
	rel = filepath.ToSlash(rel)

	dest := rel
	if hadBrackets {
		dest = "<" + rel + ">"
	}
	newTarget := dest + anchor
	if newTarget == target {
		return target, false
	}
	return newTarget, true
}

// rewriteLinksInContent rewrites inline markdown links in content.
// oldBase is the directory the file occupied before the move; newBase is after.
// srcPath is the moved-subtree root (empty for single-file moves): links whose
// targets resolve inside srcPath are left unchanged.
func rewriteLinksInContent(content []byte, oldBase, newBase, srcPath string) ([]byte, bool) {
	protected := protectedRanges(content)
	changed := false

	result := rewriteWithIndex(content, mdLinkRe, func(match []byte, start int) []byte {
		if inProtected(start, protected) {
			return match
		}
		sub := mdLinkRe.FindSubmatch(match)
		if sub == nil {
			return match
		}
		label := sub[1]
		target := string(sub[2])

		newTarget, rewrote := rewriteTarget(target, oldBase, newBase, srcPath)
		if !rewrote {
			return match
		}
		changed = true
		return append(label, []byte("("+newTarget+")")...)
	})

	result, refChanged := rewriteRefDefs(result, protectedRanges(result), func(target string) (string, bool) {
		return rewriteTarget(target, oldBase, newBase, srcPath)
	})
	if refChanged {
		changed = true
	}

	return result, changed
}

// rewriteExternalLinksToMoved rewrites links in an unmoved file that point to
// a file/directory that moved from oldPath to newPath.
func rewriteExternalLinksToMoved(content []byte, fileDir, oldPath, newPath string) ([]byte, bool) {
	protected := protectedRanges(content)
	changed := false

	rewriteFn := func(target string) (string, bool) {
		if !isRelative(target) {
			return target, false
		}
		rawPath, anchor := splitAnchor(target)
		if rawPath == "" {
			return target, false
		}
		inner, hadBrackets := stripAngleBrackets(rawPath)
		if inner == "" {
			return target, false
		}

		abs := filepath.Clean(filepath.Join(fileDir, inner))

		if !pathMatchesMoved(abs, oldPath) {
			return target, false
		}

		suffix := strings.TrimPrefix(abs, oldPath)
		newAbs := newPath + suffix
		rel, err := filepath.Rel(fileDir, newAbs)
		if err != nil {
			return target, false
		}
		rel = filepath.ToSlash(rel)

		dest := rel
		if hadBrackets {
			dest = "<" + rel + ">"
		}
		newTarget := dest + anchor
		if newTarget == target {
			return target, false
		}
		return newTarget, true
	}

	result := rewriteWithIndex(content, mdLinkRe, func(match []byte, start int) []byte {
		if inProtected(start, protected) {
			return match
		}
		sub := mdLinkRe.FindSubmatch(match)
		if sub == nil {
			return match
		}
		label := sub[1]
		target := string(sub[2])

		newTarget, rewrote := rewriteFn(target)
		if !rewrote {
			return match
		}
		changed = true
		return append(label, []byte("("+newTarget+")")...)
	})

	result, refChanged := rewriteRefDefs(result, protectedRanges(result), rewriteFn)
	if refChanged {
		changed = true
	}

	return result, changed
}

// rewriteWithIndex is like bytes.ReplaceAll via a regexp, but the replacement
// callback receives the byte offset of the match so callers can check protected
// ranges.
func rewriteWithIndex(content []byte, re *regexp.Regexp, fn func(match []byte, start int) []byte) []byte {
	locs := re.FindAllIndex(content, -1)
	if len(locs) == 0 {
		return content
	}

	var buf bytes.Buffer
	pos := 0
	for _, loc := range locs {
		buf.Write(content[pos:loc[0]])
		replacement := fn(content[loc[0]:loc[1]], loc[0])
		buf.Write(replacement)
		pos = loc[1]
	}
	buf.Write(content[pos:])
	return buf.Bytes()
}

// rewriteRefDefs rewrites reference-style link definitions of the form:
//
//	[label]: path "optional title"
func rewriteRefDefs(content []byte, protected [][2]int, rewriteFn func(string) (string, bool)) ([]byte, bool) {
	locs := mdRefDefRe.FindAllSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return content, false
	}

	changed := false
	var buf bytes.Buffer
	pos := 0
	for _, loc := range locs {
		matchStart := loc[0]
		matchEnd := loc[1]

		if inProtected(matchStart, protected) {
			buf.Write(content[pos:matchEnd])
			pos = matchEnd
			continue
		}

		prefix := content[loc[2]:loc[3]]
		target := string(content[loc[4]:loc[5]])
		suffix := content[loc[6]:loc[7]]

		newTarget, rewrote := rewriteFn(target)
		buf.Write(content[pos:matchStart])
		if rewrote {
			changed = true
			buf.Write(prefix)
			buf.WriteString(newTarget)
			buf.Write(suffix)
		} else {
			buf.Write(content[matchStart:matchEnd])
		}
		pos = matchEnd
	}
	buf.Write(content[pos:])
	return buf.Bytes(), changed
}

func pathMatchesMoved(absPath, movedPath string) bool {
	if absPath == movedPath {
		return true
	}
	return strings.HasPrefix(absPath, movedPath+string(filepath.Separator))
}

func collectMDFiles(root string) ([]string, error) {
	root = filepath.Clean(root)
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			// Do not descend into nested git repositories (submodules or
			// worktrees). Link rewriting only owns files tracked by the repo
			// rooted at root; editing another repo's files cannot be staged into
			// this repo's commit and would leave that repo with dirty,
			// uncommitted changes.
			if path != root && isGitRepoRoot(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// isGitRepoRoot reports whether dir is the root of a separate git repository,
// detected by the presence of a .git entry. For submodules and worktrees .git
// is a file pointing at the parent gitdir; for a standalone clone it is a
// directory. Either way the directory belongs to a different repository.
func isGitRepoRoot(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func collectMDFilesUnder(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(root, ".md") {
			return []string{root}, nil
		}
		return nil, nil
	}
	return collectMDFiles(root)
}

func rewriteFile(path string, rewriteFn func([]byte) ([]byte, bool)) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, camperrors.Wrap(err, "reading file")
	}
	updated, changed := rewriteFn(content)
	if !changed {
		return false, nil
	}
	if err := fsutil.WriteFileAtomically(path, updated, 0644); err != nil {
		return false, camperrors.Wrap(err, "writing file")
	}
	return true, nil
}

// modifiedFileSet accumulates campaign-root-relative, slash-separated paths of
// files a rewrite actually modified, deduplicated and sorted on read.
type modifiedFileSet struct {
	campaignRoot string
	seen         map[string]struct{}
	paths        []string
}

func newModifiedFileSet(campaignRoot string) *modifiedFileSet {
	return &modifiedFileSet{campaignRoot: campaignRoot, seen: make(map[string]struct{})}
}

func (m *modifiedFileSet) add(absPath string) {
	rel, err := filepath.Rel(m.campaignRoot, absPath)
	if err != nil {
		rel = absPath
	}
	rel = filepath.ToSlash(rel)
	if _, ok := m.seen[rel]; ok {
		return
	}
	m.seen[rel] = struct{}{}
	m.paths = append(m.paths, rel)
}

func (m *modifiedFileSet) sorted() []string {
	sort.Strings(m.paths)
	return m.paths
}

// Move describes a single file or directory move from Src to Dst, used to
// batch external-link rewriting across many moves into a single tree scan.
type Move struct {
	Src string
	Dst string
}

// RewriteForMove updates relative markdown links after a file or directory is
// moved from srcPath to dstPath, rewriting both the moved files' own links and
// links in other campaign files that pointed at the moved item. It returns the
// campaign-root-relative, slash-separated paths of every file it modified, so
// callers can stage them into the same commit as the move.
//
// For a crawl that performs many moves, prefer RewriteMovedInternalLinks per
// move plus a single RewriteExternalLinksForMoves over all moves, which scans
// the campaign tree once instead of once per move.
func RewriteForMove(ctx context.Context, campaignRoot, srcPath, dstPath string) ([]string, error) {
	internal, err := RewriteMovedInternalLinks(ctx, campaignRoot, srcPath, dstPath)
	if err != nil {
		return nil, err
	}
	external, err := RewriteExternalLinksForMoves(ctx, campaignRoot, []Move{{Src: srcPath, Dst: dstPath}})
	if err != nil {
		return nil, err
	}
	return mergeRelPaths(internal, external), nil
}

// RewriteMovedInternalLinks rewrites the relative links inside the markdown
// files that were moved from srcPath to dstPath, so links that pointed outside
// the moved subtree still resolve from the new location. It does not touch any
// file outside the moved subtree, so it is cheap regardless of campaign size.
// It returns the campaign-root-relative, slash-separated paths it modified.
func RewriteMovedInternalLinks(ctx context.Context, campaignRoot, srcPath, dstPath string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	srcPath = filepath.Clean(srcPath)
	dstPath = filepath.Clean(dstPath)

	if srcPath == dstPath {
		return nil, nil
	}

	movedMD, err := collectMDFilesUnder(dstPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "collecting moved md files")
	}

	modified := newModifiedFileSet(campaignRoot)
	for _, mdFile := range movedMD {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}
		oldBase := filepath.Dir(filepath.Join(srcPath, strings.TrimPrefix(mdFile, dstPath)))
		newBase := filepath.Dir(mdFile)
		if oldBase == newBase {
			continue
		}
		changed, err := rewriteFile(mdFile, func(b []byte) ([]byte, bool) {
			return rewriteLinksInContent(b, oldBase, newBase, srcPath)
		})
		if err != nil {
			return nil, camperrors.Wrapf(err, "rewriting links in %s", mdFile)
		}
		if changed {
			modified.add(mdFile)
		}
	}

	return modified.sorted(), nil
}

// RewriteExternalLinksForMoves rewrites links in campaign markdown files that
// pointed at any moved item, scanning the campaign tree a single time for all
// moves. A file that itself moved as part of move m has its own links handled
// by RewriteMovedInternalLinks, so m is skipped for that file; links between
// two distinct moved items are still rewritten. It returns the
// campaign-root-relative, slash-separated paths it modified.
func RewriteExternalLinksForMoves(ctx context.Context, campaignRoot string, moves []Move) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	cleaned := make([]Move, 0, len(moves))
	for _, m := range moves {
		src := filepath.Clean(m.Src)
		dst := filepath.Clean(m.Dst)
		if src == dst {
			continue
		}
		cleaned = append(cleaned, Move{Src: src, Dst: dst})
	}
	if len(cleaned) == 0 {
		return nil, nil
	}

	allMD, err := collectMDFiles(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "collecting campaign md files")
	}

	modified := newModifiedFileSet(campaignRoot)
	for _, mdFile := range allMD {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}
		cleanFile := filepath.Clean(mdFile)
		fileDir := filepath.Dir(mdFile)
		changed, err := rewriteFile(mdFile, func(b []byte) ([]byte, bool) {
			out := b
			any := false
			for _, m := range cleaned {
				if pathMatchesMoved(cleanFile, m.Dst) {
					continue
				}
				rewritten, didChange := rewriteExternalLinksToMoved(out, fileDir, m.Src, m.Dst)
				if didChange {
					out = rewritten
					any = true
				}
			}
			return out, any
		})
		if err != nil {
			return nil, camperrors.Wrapf(err, "rewriting external links in %s", mdFile)
		}
		if changed {
			modified.add(mdFile)
		}
	}

	return modified.sorted(), nil
}

// mergeRelPaths returns the sorted, deduplicated union of two slices of
// campaign-root-relative paths.
func mergeRelPaths(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, p := range append(append([]string{}, a...), b...) {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
