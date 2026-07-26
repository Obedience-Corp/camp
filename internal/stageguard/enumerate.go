package stageguard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Candidate is one path a stage-everything operation would touch, with the
// size lstat reports for it. Directories never appear: -uall expands untracked
// directories into their individual files.
type Candidate struct {
	// Path is repo-relative and slash-separated.
	Path string
	// Size is the worktree size in bytes. Symlinks report their own size; the
	// target is never followed.
	Size int64
	// Untracked is true for paths git does not yet know about. Tracked paths
	// are never excluded, so the distinction decides which guard applies.
	Untracked bool
}

// Enumerate lists what a stage-everything operation in repoPath would add.
// It runs its own git status rather than reusing internal/git/porcelain.go:
// internal/git imports this package, so importing it back would restore the
// cycle the leaf position exists to break. The duplication is a few lines and
// is deliberate.
//
// Enumeration is stat-only. Nothing is hashed, opened, or read.
func Enumerate(ctx context.Context, repoPath string) ([]Candidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	out, err := statusPorcelain(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	entries := parseStatusPorcelainZ(out)
	candidates := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		candidate, ok := classify(repoPath, entry)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// statusPorcelain runs git status in repoPath. Ignored files are absent by
// construction, which is why the commit path structurally cannot see content
// the user gitignored long ago; the doctor bigfiles sweep covers that instead.
func statusPorcelain(ctx context.Context, repoPath string) ([]byte, error) {
	args := []string{"-C", repoPath, "status", "--porcelain=v1", "-z", "-uall"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git status --porcelain=v1 -z -uall in %s: %s",
			repoPath, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// gitEnv forces the C locale so status codes and any error text stay parseable
// regardless of the user's locale, matching internal/git's exec setup.
func gitEnv(base []string) []string {
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		if strings.HasPrefix(item, "LC_ALL=") || strings.HasPrefix(item, "LANG=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "LC_ALL=C", "LANG=C")
}

// statusEntry is one record from porcelain v1 -z output.
type statusEntry struct {
	// Code is the two-character XY status code, leading space preserved.
	Code string
	Path string
}

// parseStatusPorcelainZ parses NUL-delimited porcelain v1 output. Paths are
// never quoted or escaped in -z mode, so a path containing spaces, quotes, or
// non-ASCII bytes arrives verbatim and needs no unquoting.
func parseStatusPorcelainZ(out []byte) []statusEntry {
	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	entries := make([]statusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 4 {
			continue
		}
		code := string(field[:2])
		entries = append(entries, statusEntry{Code: code, Path: string(field[3:])})
		if code[0] == 'R' || code[0] == 'C' {
			// -z emits the source path as a separate trailing field. The
			// destination is what would be staged, so the source is skipped.
			i++
		}
	}
	return entries
}

// classify turns a status entry into a candidate, or reports that it is not
// one.
func classify(repoPath string, entry statusEntry) (Candidate, bool) {
	untracked, ok := classifyEntry(entry)
	if !ok {
		return Candidate{}, false
	}
	return statCandidate(repoPath, entry.Path, untracked)
}

// classifyEntry decides whether a status entry is something a stage-everything
// operation would add, and whether it is untracked. Deletions have nothing to
// size, and unmerged entries are a conflict state the guard has no business
// acting on.
func classifyEntry(entry statusEntry) (untracked, ok bool) {
	if len(entry.Code) != 2 || entry.Path == "" {
		return false, false
	}
	x, y := entry.Code[0], entry.Code[1]

	switch {
	case x == '?' && y == '?':
		return true, true
	case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
		// Unmerged: leave conflict resolution alone.
		return false, false
	case x == 'D' || y == 'D':
		return false, false
	case x == '!' || y == '!':
		// Ignored; only reachable with --ignored, which this package never passes.
		return false, false
	default:
		// Everything else is in the index, including a file the user staged by
		// hand that has never been committed ("A "). Such a file has no history
		// to bifurcate, so the design's tracked-file rationale does not reach
		// it, but the outcome is the same for a mechanical reason: the guard
		// excludes by staging everything *except* a path, which cannot un-add
		// something already in the index. Camp reports it and commits it rather
		// than silently reversing an explicit git add.
		return false, true
	}
}

// statCandidate lstats a repo-relative path. A path that vanished between the
// status call and the stat is dropped rather than erroring: it cannot be
// staged either, so it is not the guard's problem.
func statCandidate(repoPath, rel string, untracked bool) (Candidate, bool) {
	abs := filepath.Join(repoPath, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if err != nil || info.IsDir() {
		return Candidate{}, false
	}
	return Candidate{
		Path:      filepath.ToSlash(rel),
		Size:      info.Size(),
		Untracked: untracked,
	}, true
}
