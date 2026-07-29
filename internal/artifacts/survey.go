package artifacts

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// RootSurvey counts what a directory holds, split the way declaring it as an
// artifact root would split it.
//
// The split is git's own, per WI-77a632 Proposal A: inside a declared root,
// tracked files belong to git and everything else is the artifact set. Asking
// git rather than inferring from names or extensions means the answer stays
// correct as files change class.
type RootSurvey struct {
	// Tracked files stay with git and are excluded from artifact sync.
	Tracked int
	// Untracked files are the artifact set rsync carries between machines.
	Untracked int
	// Ignored files are already invisible to git.
	Ignored int
	// UntrackedBytes is the size of the artifact set alone. Reported when
	// describing what rsync would carry, which is not the same number as the
	// directory's total: tracked scripts and ignored scratch files live in the
	// same directory but are not artifacts.
	UntrackedBytes int64
	// TotalBytes is the size of every counted file, from lstat. Symlinks
	// report their own size and are never followed.
	TotalBytes int64
}

// TotalFiles is every file the survey counted, in all three classes.
func (s RootSurvey) TotalFiles() int {
	return s.Tracked + s.Untracked + s.Ignored
}

// Mixed reports whether the root holds git-tracked content. A mixed root must
// never be blanket-gitignored: the rule would hide tracked files from git
// without untracking them.
func (s RootSurvey) Mixed() bool {
	return s.Tracked > 0
}

// SurveyRoot inspects rel under campRoot and reports its tracked, untracked,
// and ignored split with a total byte count.
//
// It runs three git queries rather than walking and classifying by hand, so
// nested .gitignore files, negation rules, and the index are git's answer.
// Sizing is lstat only: nothing is opened, read, or hashed, which is what
// makes this cheap enough to run before declaring a multi-gigabyte directory.
func SurveyRoot(ctx context.Context, campRoot, rel string) (RootSurvey, error) {
	if ctx.Err() != nil {
		return RootSurvey{}, ctx.Err()
	}
	normalized := NormalizeRootPath(rel)

	// One query per class, in the order the counts are assigned below.
	classes := [][]string{
		{"-c"},                             // tracked
		{"-o", "--exclude-standard"},       // untracked: the artifact set
		{"-o", "-i", "--exclude-standard"}, // ignored
	}

	var survey RootSurvey
	counts := []*int{&survey.Tracked, &survey.Untracked, &survey.Ignored}
	const untrackedClass = 1
	seen := make(map[string]bool)

	for i, class := range classes {
		paths, err := lsFiles(ctx, campRoot, normalized, class)
		if err != nil {
			return RootSurvey{}, err
		}
		*counts[i] = len(paths)
		for _, p := range paths {
			size := fileSize(campRoot, p)
			if i == untrackedClass {
				survey.UntrackedBytes += size
			}
			// A path can in principle surface in more than one query; size it
			// once so TotalBytes cannot double-count.
			if seen[p] {
				continue
			}
			seen[p] = true
			survey.TotalBytes += size
		}
	}
	return survey, nil
}

// lsFiles runs one git ls-files query scoped to rel and returns its paths.
func lsFiles(ctx context.Context, campRoot, rel string, args []string) ([]string, error) {
	full := append([]string{"-C", campRoot, "ls-files", "-z"}, args...)
	full = append(full, "--", filepath.FromSlash(rel))

	cmd := exec.CommandContext(ctx, "git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "git ls-files %s in %s: %s",
			strings.Join(args, " "), campRoot, strings.TrimSpace(stderr.String()))
	}

	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		paths = append(paths, string(field))
	}
	return paths, nil
}

// fileSize lstats a repo-relative path. A path that cannot be stated
// contributes zero rather than failing the survey: a broken symlink or a file
// removed mid-walk should not stop the user from seeing the totals.
func fileSize(campRoot, rel string) int64 {
	info, err := os.Lstat(filepath.Join(campRoot, filepath.FromSlash(rel)))
	if err != nil {
		return 0
	}
	return info.Size()
}
