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
// size lstat reports for it. Directories never appear here: -uall expands an
// untracked directory into its individual files.
//
// The exception is the reason the nested-repository guard exists. Git does not
// expand an untracked directory that is itself a git repository; it reports the
// directory alone, because staging it would record a gitlink rather than its
// contents. Those entries are collected separately by enumerate, since they are
// not files and have no size to measure.
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
	candidates, _, err := enumerate(ctx, repoPath)
	return candidates, err
}

// enumerate returns the file candidates and, separately, the untracked
// directories git declined to expand. Both come from one status call: running
// it twice would double the cost of every guarded stage to learn something the
// first call already reported.
func enumerate(ctx context.Context, repoPath string) ([]Candidate, []string, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	out, err := statusPorcelain(ctx, repoPath)
	if err != nil {
		return nil, nil, err
	}

	entries := parseStatusPorcelainZ(out)
	candidates := make([]Candidate, 0, len(entries))
	var dirs []string
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		candidate, kind := classify(repoPath, entry)
		switch kind {
		case entryFile:
			candidates = append(candidates, candidate)
		case entryUntrackedDir:
			dirs = append(dirs, candidate.Path)
		}
	}
	return candidates, dirs, nil
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

// entryKind sorts a status entry into what the guard does with it.
type entryKind int

const (
	// entryIgnored is a status entry no stage-everything operation would add,
	// or one that vanished before it could be measured.
	entryIgnored entryKind = iota
	// entryFile is an ordinary path with a size the per-file guards measure.
	entryFile
	// entryUntrackedDir is an untracked directory git did not expand. Under
	// -uall that means git treats it as an opaque unit, which in practice means
	// an embedded repository.
	entryUntrackedDir
)

// classify turns a status entry into a candidate and says which kind it is.
func classify(repoPath string, entry statusEntry) (Candidate, entryKind) {
	untracked, ok := classifyEntry(entry)
	if !ok {
		return Candidate{}, entryIgnored
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
//
// Git reports an unexpanded directory with a trailing slash. It is stripped
// here so the path matches the spelling every other layer uses: the pathspec
// that excludes it, the .gitmodules declaration compared against it, and the
// line the user reads.
func statCandidate(repoPath, rel string, untracked bool) (Candidate, entryKind) {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
	if rel == "" {
		return Candidate{}, entryIgnored
	}
	abs := filepath.Join(repoPath, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if err != nil {
		return Candidate{}, entryIgnored
	}
	if info.IsDir() {
		// Only untracked directories are candidates for the nested-repository
		// guard. A tracked gitlink is an existing submodule reporting a changed
		// HEAD, which is ordinary work and never the guard's business.
		if !untracked {
			return Candidate{}, entryIgnored
		}
		return Candidate{Path: rel, Untracked: true}, entryUntrackedDir
	}
	return Candidate{
		Path:      rel,
		Size:      info.Size(),
		Untracked: untracked,
	}, entryFile
}
