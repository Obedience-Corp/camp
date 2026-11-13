package prune

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

const (
	// SavedRefPrefix holds one ref per worktree whose uncommitted edits prune
	// saved before removing it.
	SavedRefPrefix = "refs/camp/pruned/"

	// SavedRetention is how long saved edits are kept before a later prune
	// pass drops them.
	SavedRetention = 30 * 24 * time.Hour

	savedBranchTrailer   = "Camp-Pruned-Branch: "
	savedWorktreeTrailer = "Camp-Pruned-Worktree: "
	savedIdentityName    = "camp prune"
	savedIdentityEmail   = "camp-prune@localhost"
)

// SavedEdits describes uncommitted edits prune kept from a removed worktree.
type SavedEdits struct {
	Branch   string
	Worktree string
	Ref      string
	Commit   string
	SavedAt  time.Time
	Expires  time.Time
	Bytes    int64
}

// savedRefName gives every branch a ref exactly one level below the prefix, so
// the refs for "a" and "a/b" can coexist.
func savedRefName(branch string) string {
	r := strings.NewReplacer("%", "%25", "/", "%2F")
	return SavedRefPrefix + r.Replace(branch)
}

// saveWorktreeEdits records the worktree's whole uncommitted state (staged,
// unstaged and untracked files; ignored files are left out) as one commit on
// top of its HEAD and points the branch's saved ref at it.
//
// The worktree is not touched: the state is staged in a scratch copy of its
// index, so a later failure leaves it exactly as it was.
func saveWorktreeEdits(ctx context.Context, projectPath, wtPath, branch string) (string, error) {
	ref := savedRefName(branch)
	if _, err := git.Output(ctx, projectPath, "rev-parse", "--verify", "--quiet", ref); err == nil {
		return "", camperrors.Newf("saved edits for %s already exist; restore or drop them first", branch)
	}

	indexPath, err := git.Output(ctx, wtPath, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", camperrors.Wrap(err, "locate worktree index")
	}
	scratch := indexPath + ".camp-prune"
	defer func() { _ = os.Remove(scratch) }()
	if err := copyFile(indexPath, scratch); err != nil {
		return "", camperrors.Wrap(err, "copy worktree index")
	}
	env := []string{"GIT_INDEX_FILE=" + scratch}
	if err := git.RunWithEnv(ctx, wtPath, env, "add", "-A"); err != nil {
		return "", camperrors.Wrap(err, "stage uncommitted edits")
	}
	tree, err := git.OutputWithEnv(ctx, wtPath, env, "write-tree")
	if err != nil {
		return "", camperrors.Wrap(err, "write tree of uncommitted edits")
	}
	head, err := git.Output(ctx, wtPath, "rev-parse", "HEAD")
	if err != nil {
		return "", camperrors.Wrap(err, "resolve worktree HEAD")
	}
	message := "camp prune: uncommitted edits from " + branch + "\n\n" +
		savedBranchTrailer + branch + "\n" + savedWorktreeTrailer + wtPath + "\n"
	commit, err := git.Output(ctx, wtPath,
		"-c", "user.name="+savedIdentityName, "-c", "user.email="+savedIdentityEmail,
		"commit-tree", tree, "-p", head, "-m", message)
	if err != nil {
		return "", camperrors.Wrap(err, "commit uncommitted edits")
	}
	if _, err := git.Output(ctx, projectPath, "update-ref", ref, commit, ""); err != nil {
		return "", camperrors.Wrap(err, "record saved edits")
	}
	resolved, err := git.Output(ctx, projectPath, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil || resolved != commit {
		return "", camperrors.Newf("saved edits for %s did not read back", branch)
	}
	return ref, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ListSaved returns the saved edits in a project, oldest first.
func ListSaved(ctx context.Context, projectPath string) ([]SavedEdits, error) {
	out, err := git.Output(ctx, projectPath, "for-each-ref",
		"--format=%(refname)%09%(objectname)%09%(committerdate:unix)", SavedRefPrefix)
	if err != nil {
		return nil, camperrors.Wrap(err, "list saved edits")
	}
	var saved []SavedEdits
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		unix, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		entry := SavedEdits{Ref: fields[0], Commit: fields[1], SavedAt: time.Unix(unix, 0)}
		entry.Expires = entry.SavedAt.Add(SavedRetention)
		body, err := git.Output(ctx, projectPath, "show", "-s", "--format=%B", entry.Commit)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(body, "\n") {
			if v, ok := strings.CutPrefix(l, savedBranchTrailer); ok {
				entry.Branch = strings.TrimSpace(v)
			}
			if v, ok := strings.CutPrefix(l, savedWorktreeTrailer); ok {
				entry.Worktree = strings.TrimSpace(v)
			}
		}
		if entry.Branch == "" {
			continue
		}
		entry.Bytes = savedBytes(ctx, projectPath, entry.Commit)
		saved = append(saved, entry)
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].SavedAt.Before(saved[j].SavedAt) })
	return saved, nil
}

// savedBytes sums the size of the files the saved commit changed or added.
func savedBytes(ctx context.Context, projectPath, commit string) int64 {
	out, err := git.Output(ctx, projectPath, "diff-tree", "-r", "--no-renames", "--diff-filter=AM",
		"--format=", commit+"^", commit)
	if err != nil {
		return 0
	}
	var total int64
	for _, line := range strings.Split(out, "\n") {
		// ":<old mode> <new mode> <old sha> <new sha> <status>\t<path>"
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[1] == "160000" {
			continue
		}
		size, err := git.Output(ctx, projectPath, "cat-file", "-s", fields[3])
		if err != nil {
			continue
		}
		if n, err := strconv.ParseInt(size, 10, 64); err == nil {
			total += n
		}
	}
	return total
}

func findSaved(ctx context.Context, projectPath, branch string) (SavedEdits, error) {
	saved, err := ListSaved(ctx, projectPath)
	if err != nil {
		return SavedEdits{}, err
	}
	for _, s := range saved {
		if s.Branch == branch {
			return s, nil
		}
	}
	return SavedEdits{}, camperrors.NewNotFound("saved edits", branch, nil)
}

// RestoreSaved recreates the removed worktree on its branch with the saved
// edits back in place as uncommitted changes, then drops the saved ref. It
// refuses when the branch or the folder already exists.
func RestoreSaved(ctx context.Context, projectPath, branch string) (SavedEdits, error) {
	s, err := findSaved(ctx, projectPath, branch)
	if err != nil {
		return SavedEdits{}, err
	}
	if _, err := git.Output(ctx, projectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return s, camperrors.Newf("branch %s already exists; delete it or restore by hand from %s", branch, s.Ref)
	}
	if s.Worktree == "" {
		return s, camperrors.Newf("saved edits for %s do not record a worktree folder", branch)
	}
	if _, err := os.Lstat(s.Worktree); err == nil {
		return s, camperrors.Newf("%s already exists; move it or restore by hand from %s", s.Worktree, s.Ref)
	}
	if err := os.MkdirAll(filepath.Dir(s.Worktree), 0o755); err != nil {
		return s, camperrors.Wrap(err, "create worktree parent folder")
	}
	if _, err := git.Output(ctx, projectPath, "worktree", "add", "-b", branch, s.Worktree, s.Commit); err != nil {
		return s, camperrors.Wrap(err, "recreate worktree")
	}
	// The worktree now holds the saved state as a commit. Step the branch back
	// to the tip it had, leaving that state as uncommitted changes.
	if _, err := git.Output(ctx, s.Worktree, "reset", "--mixed", "--quiet", s.Commit+"^"); err != nil {
		return s, camperrors.Wrap(err, "return saved edits to uncommitted state")
	}
	if _, err := git.Output(ctx, projectPath, "update-ref", "-d", s.Ref, s.Commit); err != nil {
		return s, camperrors.Wrap(err, "drop saved ref after restore")
	}
	return s, nil
}

// DropSaved deletes the saved edits for a branch.
func DropSaved(ctx context.Context, projectPath, branch string) (SavedEdits, error) {
	s, err := findSaved(ctx, projectPath, branch)
	if err != nil {
		return SavedEdits{}, err
	}
	if _, err := git.Output(ctx, projectPath, "update-ref", "-d", s.Ref, s.Commit); err != nil {
		return s, camperrors.Wrap(err, "drop saved edits")
	}
	return s, nil
}

// expireSaved drops saved edits older than SavedRetention and returns the
// branches they belonged to.
func expireSaved(ctx context.Context, projectPath string, now time.Time) ([]string, error) {
	saved, err := ListSaved(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	var dropped []string
	for _, s := range saved {
		if now.Before(s.Expires) {
			continue
		}
		if _, err := git.Output(ctx, projectPath, "update-ref", "-d", s.Ref, s.Commit); err != nil {
			return dropped, camperrors.Wrapf(err, "expire saved edits for %s", s.Branch)
		}
		dropped = append(dropped, s.Branch)
	}
	return dropped, nil
}
