package skills

import (
	"context"
	"strings"
	"sync"

	"github.com/Obedience-Corp/camp/internal/git"
)

// Provenance supplies evidence, beyond the manifest, that a shipped skill file
// still holds what camp wrote.
type Provenance interface {
	// WrittenOnce reports whether the file at relPath, a slash path relative
	// to the camp root, is committed, unchanged since, and touched by exactly
	// one commit. It returns an error only when ctx ends; any other doubt,
	// including a git failure, reports false.
	WrittenOnce(ctx context.Context, relPath string) (bool, error)
}

// GitProvenance answers WrittenOnce from the git history of a camp root.
// Before the manifest existed camp wrote each shipped file once and never
// rewrote it, so any second commit touching the file is someone else's edit.
type GitProvenance struct {
	root    string
	mu      sync.Mutex
	loaded  bool
	written map[string]bool
}

// NewGitProvenance returns the git evidence for the camp at root. Git runs
// once, on the first question, and only over .campaign/skills/.
func NewGitProvenance(root string) *GitProvenance {
	return &GitProvenance{root: root}
}

// WrittenOnce implements Provenance. A scan that ctx cut short is not kept,
// so a later question with a live context scans again.
func (p *GitProvenance) WrittenOnce(ctx context.Context, relPath string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		written, err := writtenOnce(ctx, p.root)
		if err != nil {
			return false, err
		}
		p.written, p.loaded = written, true
	}
	return p.written[relPath], nil
}

// writtenOnce returns the camp-root-relative skill paths that are tracked,
// match HEAD in the index and the work tree, and appear in exactly one commit.
// A shallow clone hides older commits, so it offers no evidence; so does any
// git failure, such as a camp that is not a repository or has no commits. A
// failure while ctx is done returns ctx's error instead.
func writtenOnce(ctx context.Context, root string) (map[string]bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shallow, err := git.Output(ctx, root, "rev-parse", "--is-shallow-repository")
	if err != nil || shallow != "false" {
		return nil, ctx.Err()
	}
	scope := strings.TrimSuffix(shippedSkillsPrefix, "/")
	tracked, err := gitPaths(ctx, root, "ls-files", "-z", "--", scope)
	if err != nil {
		return nil, ctx.Err()
	}
	dirty, err := gitPaths(ctx, root, "diff", "--name-only", "-z", "--relative", "HEAD", "--", scope)
	if err != nil {
		return nil, ctx.Err()
	}
	staged, err := gitPaths(ctx, root, "diff", "--cached", "--name-only", "-z", "--relative", "HEAD", "--", scope)
	if err != nil {
		return nil, ctx.Err()
	}
	// -c lists a merge's own edits: files that differ from every parent.
	touched, err := gitPaths(ctx, root, "log", "--format=", "--name-only", "--no-renames", "-c", "-z", "--relative", "--", scope)
	if err != nil {
		return nil, ctx.Err()
	}

	commits := make(map[string]int, len(touched))
	for _, p := range touched {
		commits[p]++
	}
	for _, p := range append(dirty, staged...) {
		commits[p] = -1
	}
	written := make(map[string]bool)
	for _, p := range tracked {
		if commits[p] == 1 {
			written[p] = true
		}
	}
	return written, nil
}

func gitPaths(ctx context.Context, root string, args ...string) ([]string, error) {
	out, err := git.Output(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
