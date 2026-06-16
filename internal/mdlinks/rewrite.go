package mdlinks

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

var mdLinkRe = regexp.MustCompile(`(!?\[(?:[^\[\]]*|\[[^\[\]]*\])*\])\(([^)]+)\)`)

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

func rewriteLinksInContent(content []byte, oldBase, newBase string) ([]byte, bool) {
	changed := false
	result := mdLinkRe.ReplaceAllFunc(content, func(match []byte) []byte {
		sub := mdLinkRe.FindSubmatch(match)
		if sub == nil {
			return match
		}
		label := sub[1]
		target := string(sub[2])

		if !isRelative(target) {
			return match
		}

		path, anchor := splitAnchor(target)
		if path == "" {
			return match
		}

		abs := filepath.Join(oldBase, path)
		rel, err := filepath.Rel(newBase, abs)
		if err != nil {
			return match
		}
		rel = filepath.ToSlash(rel)

		newTarget := rel + anchor
		if newTarget == target {
			return match
		}

		changed = true
		return append(label, []byte("("+newTarget+")")...)
	})
	return result, changed
}

func rewriteExternalLinksToMoved(content []byte, fileDir, oldPath, newPath string) ([]byte, bool) {
	changed := false
	result := mdLinkRe.ReplaceAllFunc(content, func(match []byte) []byte {
		sub := mdLinkRe.FindSubmatch(match)
		if sub == nil {
			return match
		}
		label := sub[1]
		target := string(sub[2])

		if !isRelative(target) {
			return match
		}

		path, anchor := splitAnchor(target)
		if path == "" {
			return match
		}

		abs := filepath.Clean(filepath.Join(fileDir, path))

		if !pathMatchesMoved(abs, oldPath) {
			return match
		}

		suffix := strings.TrimPrefix(abs, oldPath)

		newAbs := newPath + suffix
		rel, err := filepath.Rel(fileDir, newAbs)
		if err != nil {
			return match
		}
		rel = filepath.ToSlash(rel)

		newTarget := rel + anchor
		if newTarget == target {
			return match
		}

		changed = true
		return append(label, []byte("("+newTarget+")")...)
	})
	return result, changed
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

func rewriteFile(path string, rewriteFn func([]byte) ([]byte, bool)) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return camperrors.Wrap(err, "reading file")
	}
	updated, changed := rewriteFn(content)
	if !changed {
		return nil
	}
	if err := fsutil.WriteFileAtomically(path, updated, 0644); err != nil {
		return camperrors.Wrap(err, "writing file")
	}
	return nil
}

func RewriteForMove(ctx context.Context, campaignRoot, srcPath, dstPath string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	srcPath = filepath.Clean(srcPath)
	dstPath = filepath.Clean(dstPath)

	if srcPath == dstPath {
		return nil
	}

	movedMD, err := collectMDFilesUnder(dstPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrap(err, "collecting moved md files")
	}

	movedSet := make(map[string]struct{}, len(movedMD))
	for _, f := range movedMD {
		movedSet[f] = struct{}{}
	}

	for _, mdFile := range movedMD {
		if err := ctx.Err(); err != nil {
			return camperrors.Wrap(err, "context cancelled")
		}
		oldBase := filepath.Dir(filepath.Join(srcPath, strings.TrimPrefix(mdFile, dstPath)))
		newBase := filepath.Dir(mdFile)
		if oldBase == newBase {
			continue
		}
		if err := rewriteFile(mdFile, func(b []byte) ([]byte, bool) {
			return rewriteLinksInContent(b, oldBase, newBase)
		}); err != nil {
			return camperrors.Wrapf(err, "rewriting links in %s", mdFile)
		}
	}

	allMD, err := collectMDFiles(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "collecting campaign md files")
	}

	for _, mdFile := range allMD {
		if err := ctx.Err(); err != nil {
			return camperrors.Wrap(err, "context cancelled")
		}
		if _, isMoved := movedSet[mdFile]; isMoved {
			continue
		}
		fileDir := filepath.Dir(mdFile)
		if err := rewriteFile(mdFile, func(b []byte) ([]byte, bool) {
			return rewriteExternalLinksToMoved(b, fileDir, srcPath, dstPath)
		}); err != nil {
			return camperrors.Wrapf(err, "rewriting external links in %s", mdFile)
		}
	}

	return nil
}
