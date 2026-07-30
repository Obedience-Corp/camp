package stageguard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// The guard's second attachment point: an index camp did not build.
//
// Everything in check.go answers "what would a stage-everything operation add
// here". A commit that stages nothing never asks it, so `camp commit
// --all=false` over an index the user assembled with git add says nothing about
// what is in it. This file answers the other question -- "what is in the index
// already" -- so that commit can report the same findings.
//
// Sizes come from the index, not from lstat. The blob is what actually enters
// the repository, so it is the more correct number: content filters have
// already run. git-LFS is the case that matters, since it replaces a large file
// with a pointer of a few hundred bytes; an LFS-managed path therefore falls
// under any threshold by construction, and needs none of the check-attr call
// that FilterLFSManaged makes on the worktree side.

// CheckStagedIndex returns what the index already holds that the per-file
// threshold flags. It is header-only: no file and no object is read.
//
// Every finding is TrackedGrowth, because everything in an index is tracked by
// definition. That is also the conclusion the staging guard reaches for a file
// the user staged by hand (see classifyEntry in enumerate.go), which is what
// lets both paths share one report and one remedy.
//
// The bulk guard deliberately does not run here. Bulk blocks, and a caller that
// has not staged anything must not: it would be refusing an index the user
// built deliberately. Reporting bulk instead would need remedy copy for a
// situation camp has already allowed to happen, which is a design question
// rather than the silence this closes.
func CheckStagedIndex(ctx context.Context, repoPath string, limits GuardLimits) ([]GuardViolation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if limits.LargeFiles == ModeOff {
		return nil, nil
	}

	candidates, err := EnumerateStaged(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	violations, _ := checkPerFile(filterAllowed(candidates, limits.Allow), limits)
	return violations, nil
}

// EnumerateStaged lists the regular files the index holds relative to HEAD,
// each with the size of the blob it would commit.
//
// Two git processes, whatever the file count: one diff to name the entries and
// their object ids, one batched cat-file to size them. Neither scales with how
// large the staged content is.
func EnumerateStaged(ctx context.Context, repoPath string) ([]Candidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	out, err := diffCachedRaw(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	entries := parseDiffRawZ(out)
	if len(entries) == 0 {
		return nil, nil
	}

	sizes, err := blobSizes(ctx, repoPath, entries)
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		size, ok := sizes[entry.OID]
		if !ok {
			// An object this repository does not have. Nothing to size, and
			// nothing camp should guess at.
			continue
		}
		// Untracked stays false: an index entry is tracked, which is what makes
		// every finding here report-only.
		candidates = append(candidates, Candidate{Path: entry.Path, Size: size})
	}
	return candidates, nil
}

// stagedEntry is one index entry a commit would write: its path and the object
// id of the blob that path points at.
type stagedEntry struct {
	Path string
	OID  string
}

// diffCachedRaw asks git what the index holds relative to HEAD.
//
// `git diff --cached` rather than `git diff-index HEAD`, because it is the form
// that works before the first commit: an unborn HEAD diffs against the empty
// tree instead of failing on an unknown revision, and a campaign's first commit
// is exactly when a user is most likely to add something large.
//
// --no-renames keeps every record to one path and one object id. Rename
// detection would pay for a similarity scan and buy nothing, since the
// destination blob is the only thing being sized. --no-abbrev gives whole
// object ids whatever hash algorithm the repository uses, so nothing downstream
// has to resolve an ambiguous prefix.
func diffCachedRaw(ctx context.Context, repoPath string) ([]byte, error) {
	args := []string{"-C", repoPath, "diff", "--cached", "--raw", "-z", "--no-renames", "--no-abbrev"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git diff --cached --raw in %s: %s",
			repoPath, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// parseDiffRawZ parses `git diff --raw -z` output.
//
// Each record is ":<src-mode> <dst-mode> <src-oid> <dst-oid> <status>", a NUL,
// then the path. Paths are never quoted or escaped in -z mode, so one holding a
// space, a quote, or a newline arrives verbatim and needs no unquoting.
func parseDiffRawZ(out []byte) []stagedEntry {
	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	entries := make([]stagedEntry, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		entry, ok := parseDiffRawRecord(string(fields[i]), string(fields[i+1]))
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseDiffRawRecord turns one metadata/path pair into an entry, or reports
// that it is not one worth sizing.
func parseDiffRawRecord(meta, path string) (stagedEntry, bool) {
	if path == "" || !strings.HasPrefix(meta, ":") {
		return stagedEntry{}, false
	}
	parts := strings.Fields(meta[1:])
	if len(parts) < 4 || !isRegularFileMode(parts[1]) {
		return stagedEntry{}, false
	}
	return stagedEntry{Path: path, OID: parts[3]}, true
}

// isRegularFileMode reports whether a record's destination is an ordinary file.
//
// The three modes it rejects each have a reason. 000000 is a deletion, which
// has no destination content to size. 160000 is a submodule gitlink, whose
// object id names a commit in the submodule's object database rather than this
// repository's, so asking for its size here is meaningless. 120000 is a
// symlink, whose blob is the target path rather than the target's bytes.
func isRegularFileMode(mode string) bool {
	return mode == "100644" || mode == "100755"
}

// blobSizes asks git for the size of each object, in one process.
//
// Object ids go in over stdin: they are hex and cannot contain a newline, so
// the line-delimited batch protocol is safe for them in a way it would not be
// for paths. Ids are deduplicated first, which also collapses the identical
// copies a large staged batch tends to carry.
//
// --batch-check reads object headers. Nothing is inflated or streamed, so a
// staged gigabyte costs the same as a staged kilobyte.
func blobSizes(ctx context.Context, repoPath string, entries []stagedEntry) (map[string]int64, error) {
	var stdin bytes.Buffer
	for _, oid := range uniqueOIDs(entries) {
		stdin.WriteString(oid)
		stdin.WriteByte('\n')
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "cat-file", "--batch-check")
	cmd.Env = gitEnv(os.Environ())
	cmd.Stdin = &stdin

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git cat-file --batch-check in %s: %s",
			repoPath, strings.TrimSpace(stderr.String()))
	}
	return parseBatchCheck(out), nil
}

// uniqueOIDs returns each object id once, in the order first seen. Two paths
// holding identical bytes share a blob, so a batch that copied a directory asks
// git about far fewer objects than it has entries.
func uniqueOIDs(entries []stagedEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	oids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.OID]; ok {
			continue
		}
		seen[entry.OID] = struct{}{}
		oids = append(oids, entry.OID)
	}
	return oids
}

// parseBatchCheck reads `git cat-file --batch-check` output: one
// "<oid> <type> <size>" line per input id.
//
// An object the repository does not have answers "<oid> missing" and is left
// out of the map rather than recorded as zero bytes, so a caller can tell "not
// there" from "empty" instead of silently sizing an absent object at nothing.
func parseBatchCheck(out []byte) map[string]int64 {
	lines := strings.Split(string(out), "\n")
	sizes := make(map[string]int64, len(lines))
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) != 3 || parts[1] != "blob" {
			continue
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		sizes[parts[0]] = size
	}
	return sizes
}
