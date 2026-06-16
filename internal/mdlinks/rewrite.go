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

// RewriteForMove updates relative markdown links after a file or directory is
// moved from srcPath to dstPath, rewriting both the moved files' own links and
// links in other campaign files that pointed at the moved item. It returns the
// campaign-root-relative, slash-separated paths of every file it modified, so
// callers can stage them into the same commit as the move.
func RewriteForMove(ctx context.Context, campaignRoot, srcPath, dstPath string) ([]string, error) {
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

	movedSet := make(map[string]struct{}, len(movedMD))
	for _, f := range movedMD {
		movedSet[f] = struct{}{}
	}

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

	allMD, err := collectMDFiles(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "collecting campaign md files")
	}

	for _, mdFile := range allMD {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}
		if _, isMoved := movedSet[mdFile]; isMoved {
			continue
		}
		fileDir := filepath.Dir(mdFile)
		changed, err := rewriteFile(mdFile, func(b []byte) ([]byte, bool) {
			return rewriteExternalLinksToMoved(b, fileDir, srcPath, dstPath)
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
