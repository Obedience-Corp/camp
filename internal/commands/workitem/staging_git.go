package workitem

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// listChangedFilesUnder returns repo-relative paths for changes inside prefix
// (or the entire repo when prefix is empty). Wraps `git status --porcelain`
// with `--untracked-files=all` so untracked directories are expanded to
// individual files (otherwise --exclude cannot target leaf paths).
func listChangedFilesUnder(ctx context.Context, repoRoot, prefix string) ([]string, error) {
	args := []string{"-C", repoRoot, "status", "--porcelain", "-z", "--untracked-files=all"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	entries, err := gitStatusPorcelainZ(ctx, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		files = append(files, entry.Path)
	}
	return files, nil
}

func listStagedFiles(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "diff", "--cached", "--name-only", "-z").Output()
	if err != nil {
		return nil, err
	}
	return parseNULPathList(out), nil
}

func pathIsDirty(ctx context.Context, repoRoot, relPath string) (bool, error) {
	entries, err := gitStatusPorcelainZ(ctx, "-C", repoRoot, "status", "--porcelain", "-z", "--", relPath)
	if err != nil {
		return false, err
	}
	return len(entries) != 0, nil
}

func listDirtySubmodulePointers(ctx context.Context, repoRoot string) ([]string, error) {
	entries, err := gitStatusPorcelainZ(ctx, "-C", repoRoot, "status", "--porcelain", "-z", "--", "projects")
	if err != nil {
		return nil, err
	}
	var pointers []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "projects/") {
			pointers = append(pointers, entry.Path)
		}
	}
	return pointers, nil
}

type gitStatusEntry struct {
	Code string
	Path string
}

func gitStatusPorcelainZ(ctx context.Context, args ...string) ([]gitStatusEntry, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, err
	}
	return parseGitStatusPorcelainZ(out), nil
}

func parseGitStatusPorcelainZ(out []byte) []gitStatusEntry {
	fields := splitNULFields(out)
	entries := make([]gitStatusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) == 0 {
			continue
		}
		if len(field) < 4 {
			continue
		}
		code := string(field[:2])
		path := string(field[3:])
		entries = append(entries, gitStatusEntry{Code: code, Path: path})
		if code[0] == 'R' || code[0] == 'C' {
			i++ // -z emits the old path as a second NUL-delimited field.
		}
	}
	return entries
}

func parseNULPathList(out []byte) []string {
	fields := splitNULFields(out)
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		paths = append(paths, string(field))
	}
	return paths
}

func splitNULFields(out []byte) [][]byte {
	return bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
}

func listSubmodulePointerSkips(ctx context.Context, root string, allowed bool) []SkippedEntry {
	if allowed {
		return nil
	}
	pointers, err := listDirtySubmodulePointers(ctx, root)
	if err != nil {
		return nil
	}
	out := make([]SkippedEntry, 0, len(pointers))
	for _, p := range pointers {
		out = append(out, SkippedEntry{Path: p, Reason: skipReasonPointerOffByDefault})
	}
	return out
}
