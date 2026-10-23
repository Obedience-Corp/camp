package skills

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
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

// GitProvenance answers WrittenOnce from the git history of a camp root. Its
// evidence is a commit camp itself made: camp init and camp repair write a
// shipped file and commit it in one process, so the blob that commit holds is
// what camp wrote. Before the manifest existed camp never rewrote a shipped
// file, so any second commit touching it is someone else's edit. Any user
// commit, including a camp commit --all sweep, could carry an edit made before
// it, so a file whose only commit is not camp's stays unknown, and so does one
// whose only commit is a repair from before repair staged only its own files.
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
// match HEAD in the index and the work tree, and appear in exactly one commit,
// a camp init or camp repair commit.
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
	history, err := skillHistory(ctx, root, scope)
	if err != nil {
		return nil, ctx.Err()
	}

	for _, p := range append(dirty, staged...) {
		delete(history, p)
	}
	written := make(map[string]bool)
	for _, p := range tracked {
		if h := history[p]; h.commits == 1 && h.byCamp {
			written[p] = true
		}
	}
	return written, nil
}

// fileHistory counts the commits that touched a file and records whether the
// oldest of them was a camp init or repair commit.
type fileHistory struct {
	commits int
	byCamp  bool
}

const commitMarker = "\x01"

// skillHistory reads every commit touching scope, with its committer date and
// subject, in one git call. Each commit prints as the marker, the strict ISO
// date, a space, and the subject, then its paths.
func skillHistory(ctx context.Context, root, scope string) (map[string]fileHistory, error) {
	// -c lists a merge's own edits: files that differ from every parent.
	out, err := git.Output(ctx, root, "log", "--format="+commitMarker+"%cI %s", "--name-only", "--no-renames", "-c", "-z", "--relative", "--", scope)
	if err != nil {
		return nil, err
	}
	history := make(map[string]fileHistory)
	byCamp := false
	for field := range strings.SplitSeq(out, "\x00") {
		if header, ok := strings.CutPrefix(field, commitMarker); ok {
			date, subject, _ := strings.Cut(header, " ")
			committed, _ := time.Parse(time.RFC3339, date)
			byCamp = commit.IsScaffoldCommit(subject, committed)
			continue
		}
		path := strings.TrimPrefix(field, "\n")
		if path == "" {
			continue
		}
		h := history[path]
		h.commits++
		h.byCamp = byCamp
		history[path] = h
	}
	return history, nil
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
