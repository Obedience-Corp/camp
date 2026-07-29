package stageguard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// lfsFilter is the check-attr value marking a path as git-LFS managed.
const lfsFilter = "lfs"

// FilterLFSManaged removes paths a `filter=lfs` attribute already covers.
// The exemption applies in every mode and needs no configuration: a user on
// git LFS has already solved this problem, and camp interfering would make
// their setup worse rather than safer.
//
// It runs before any violation is recorded, so an LFS-managed path is never
// reported, never excluded, and never counted toward the bulk trigger.
func FilterLFSManaged(ctx context.Context, repoPath string, candidates []Candidate) ([]Candidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	managed, err := lfsManagedPaths(ctx, repoPath, candidates)
	if err != nil {
		return nil, err
	}
	if len(managed) == 0 {
		return candidates, nil
	}

	kept := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if managed[candidate.Path] {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, nil
}

// lfsManagedPaths asks git which candidates carry filter=lfs. Paths go in over
// stdin so the batch is not bounded by the platform argument limit, which a
// bulk directory would otherwise blow past.
func lfsManagedPaths(ctx context.Context, repoPath string, candidates []Candidate) (map[string]bool, error) {
	var stdin bytes.Buffer
	for _, candidate := range candidates {
		stdin.WriteString(candidate.Path)
		stdin.WriteByte(0)
	}

	args := []string{"-C", repoPath, "check-attr", "-z", "--stdin", "filter"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv(os.Environ())
	cmd.Stdin = &stdin

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git check-attr filter in %s: %s",
			repoPath, strings.TrimSpace(stderr.String()))
	}
	return parseCheckAttrZ(out), nil
}

// parseCheckAttrZ reads check-attr -z output, which is a flat stream of
// (path, attribute, value) NUL-terminated triples.
func parseCheckAttrZ(out []byte) map[string]bool {
	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	managed := make(map[string]bool)
	for i := 0; i+2 < len(fields); i += 3 {
		if string(fields[2+i]) == lfsFilter {
			managed[string(fields[i])] = true
		}
	}
	return managed
}
