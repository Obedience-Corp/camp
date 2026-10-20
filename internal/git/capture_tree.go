package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ChangedNames lists paths that differ from HEAD, staged or not.
//
// Untracked files are absent. An unborn HEAD has no base to diff, so this
// returns no names rather than an error the caller would have to special-case.
// Rename detection is off: a staged rename lists both paths, so a tree built
// from these names drops the old path instead of keeping it beside the new.
func ChangedNames(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !headResolvable(ctx, repoPath) {
		return nil, nil
	}
	out, err := Output(ctx, repoPath, "diff", "--name-only", "-z", "--no-renames", "HEAD")
	if err != nil {
		return nil, camperrors.Wrap(err, "list changes against HEAD")
	}
	return splitNull(out), nil
}

// UntrackedCollapsed lists untracked paths, with untracked directories kept
// as one entry ending in "/".
//
// --directory is the point. A repeat commit must not expand a media tree
// into every file inside it just to learn the directory is still untracked.
// Empty directories are left out, as git status leaves them out: there is
// nothing in them to commit or to guard.
func UntrackedCollapsed(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out, err := Output(ctx, repoPath, "ls-files", "-z", "-o", "--exclude-standard", "--directory", "--no-empty-directory")
	if err != nil {
		return nil, camperrors.Wrap(err, "list untracked paths")
	}
	return splitNull(out), nil
}

func splitNull(out string) []string {
	if out == "" {
		return nil
	}
	// Accept both NUL and newline separators. A command that ignored -z must
	// not collapse the whole listing into one path.
	out = strings.ReplaceAll(out, "\x00", "\n")
	var names []string
	for _, part := range strings.Split(out, "\n") {
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

// CaptureTree records paths on top of HEAD in a temporary index and returns
// that tree. The repository's real index is not touched.
//
// Paths are file names from git listings, so they are added literally: an
// untracked file named "*.mp4" must not pull in every video in the camp.
// roots are declared artifact roots; the directory-link scan skips them
// because the root plan already classified their links.
func CaptureTree(ctx context.Context, repoPath string, paths, roots []string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	tmp, _, err := BuildTempIndexPath(repoPath)
	if err != nil {
		return "", err
	}
	defer RemoveTempIndex(tmp)
	if err := ReadTreeIntoTempIndex(ctx, repoPath, tmp); err != nil {
		return "", err
	}
	if len(paths) > 0 {
		if err := addPathsToTempIndex(ctx, repoPath, tmp, paths, nil, true, roots); err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "write-tree")
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmp)
	out, err := cmd.Output()
	if err != nil {
		return "", camperrors.Wrap(err, "capture a tree without staging")
	}
	tree := strings.TrimSpace(string(out))
	if tree == "" {
		return "", camperrors.New("git write-tree produced no tree")
	}
	return tree, nil
}

// OperationInProgress reports a merge, rebase, cherry-pick, or revert under
// way, or unmerged entries in the index.
//
// A captured tree is committed on one parent. Installing it in the middle of
// any of these would drop the second parent or the conflict stages, so the
// caller has to stage in the foreground instead.
func OperationInProgress(ctx context.Context, repoPath string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	gitDir, err := ResolveGitDir(repoPath)
	if err != nil {
		return false, err
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "REBASE_HEAD", "rebase-merge", "rebase-apply"} {
		if _, statErr := os.Lstat(filepath.Join(gitDir, marker)); statErr == nil {
			return true, nil
		}
	}
	out, err := Output(ctx, repoPath, "ls-files", "-z", "--unmerged")
	if err != nil {
		return false, camperrors.Wrap(err, "list unmerged index entries")
	}
	return out != "", nil
}

// InstallTree makes the real index match tree.
//
// The commit-tree worker only moves HEAD. It does not touch the index, and
// its contract is that the index already holds this snapshot.
//
// read-tree --reset keeps the cached stat data of every entry whose blob is
// unchanged, so the next status does not rehash the whole tree. Plain
// read-tree drops it, and -m refuses whenever a changed path also has
// unstaged edits. --reset discards conflict stages, which is why callers
// check OperationInProgress first.
//
// finish(false) puts the previous index back when the job could not be
// queued, unless something else rewrote the index since, which the index
// checksum shows. finish(true) drops the backup and keeps the snapshot.
func InstallTree(ctx context.Context, repoPath, tree string) (finish func(keep bool) error, err error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	gitDir, err := ResolveGitDir(repoPath)
	if err != nil {
		return nil, err
	}
	real := filepath.Join(gitDir, "index")
	backup, err := backupIndex(gitDir, real)
	if err != nil {
		return nil, err
	}
	dropBackup := func() error {
		if backup == "" {
			return nil
		}
		if removeErr := os.Remove(backup); removeErr != nil && !os.IsNotExist(removeErr) {
			return camperrors.Wrapf(removeErr, "remove index backup %s", backup)
		}
		return nil
	}

	cfg := DefaultRetryConfig()
	cfg.OperationName = "install-tree"
	if err := WithLockRetry(ctx, repoPath, cfg, func() error {
		return runIndexCommand(ctx, repoPath, "read-tree", "read-tree", "--reset", tree)
	}); err != nil {
		// git writes the index through index.lock, so a failed read-tree left
		// it as it was. Restoring the backup could undo another writer.
		_ = dropBackup()
		return nil, camperrors.Wrap(err, "install captured tree into the index")
	}
	hashLen := indexHashLen(ctx, repoPath)
	installed, sumErr := indexFingerprint(real, hashLen)
	if sumErr != nil {
		_ = dropBackup()
		return nil, sumErr
	}

	finish = func(keep bool) error {
		if keep {
			return dropBackup()
		}
		current, curErr := indexFingerprint(real, hashLen)
		if curErr != nil || !bytes.Equal(current, installed) {
			_ = dropBackup()
			return camperrors.New("the index changed after the captured tree was installed; left it as it is")
		}
		if backup == "" {
			if removeErr := os.Remove(real); removeErr != nil && !os.IsNotExist(removeErr) {
				return camperrors.Wrapf(removeErr, "remove installed index %s", real)
			}
			return nil
		}
		if renameErr := os.Rename(backup, real); renameErr != nil {
			return camperrors.Wrapf(renameErr, "restore index from %s", backup)
		}
		return nil
	}
	return finish, nil
}

// indexHashLen is the length of the checksum git ends the index with: the
// repository's object hash.
func indexHashLen(ctx context.Context, repoPath string) int {
	if format, err := Output(ctx, repoPath, "rev-parse", "--show-object-format"); err == nil && format == "sha256" {
		return sha256.Size
	}
	return sha1ChecksumLen
}

// sha1ChecksumLen is the index checksum length in a SHA-1 repository.
const sha1ChecksumLen = 20

// indexFingerprint returns the checksum git writes at the end of the index,
// which changes whenever its content does. An index written with
// index.skipHash ends in zeros instead, so its whole content is hashed.
func indexFingerprint(path string, hashLen int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, camperrors.Wrapf(err, "open index %s", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, camperrors.Wrapf(err, "stat index %s", path)
	}
	if info.Size() < int64(hashLen) {
		return nil, camperrors.Newf("index %s is shorter than its checksum", path)
	}
	trailer := make([]byte, hashLen)
	if _, err := f.ReadAt(trailer, info.Size()-int64(hashLen)); err != nil {
		return nil, camperrors.Wrapf(err, "read index checksum %s", path)
	}
	if !bytes.Equal(trailer, make([]byte, hashLen)) {
		return trailer, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, info.Size())); err != nil {
		return nil, camperrors.Wrapf(err, "hash index %s", path)
	}
	return h.Sum(nil), nil
}

// backupIndex copies the real index to a uniquely named file beside it, so
// two commits queueing at once cannot overwrite each other's backup. It
// returns "" when there is no index yet.
func backupIndex(gitDir, real string) (string, error) {
	if _, err := os.Stat(real); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", camperrors.Wrapf(err, "read index %s", real)
	}
	f, err := os.CreateTemp(gitDir, "index.camp-before-queue.*")
	if err != nil {
		return "", camperrors.Wrapf(err, "create index backup in %s", gitDir)
	}
	backup := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(backup)
		return "", camperrors.Wrapf(err, "close index backup %s", backup)
	}
	if err := CopyFile(real, backup); err != nil {
		_ = os.Remove(backup)
		return "", err
	}
	return backup, nil
}

// runIndexCommand runs a git command that writes the real index, turning a
// held index.lock into a LockError so WithLockRetry can wait it out.
func runIndexCommand(ctx context.Context, repoPath, operation string, args ...string) error {
	cmd := gitCmd(ctx, repoPath, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	errType := ClassifyGitError(string(out), exitCode)
	if errType == GitErrorLock {
		return &LockError{Path: "index.lock", Err: err}
	}
	return camperrors.NewGit(operation, "", errType.String(), strings.TrimSpace(string(out)), err)
}

// AddPathsToTempIndex stages paths into the provided temp index.
//
// retry is optional: nil takes the interactive profile.
func AddPathsToTempIndex(ctx context.Context, repoPath, tmpPath string, paths []string, retry *RetryConfig) error {
	return addPathsToTempIndex(ctx, repoPath, tmpPath, paths, retry, false, nil)
}

// addPathsToTempIndex is AddPathsToTempIndex with an option to treat every
// path as a literal file name rather than a pathspec, and directories whose
// changes the link scan does not need to read.
func addPathsToTempIndex(ctx context.Context, repoPath, tmpPath string, paths []string, retry *RetryConfig, literal bool, skipScan []string) error {
	if len(paths) == 0 {
		return ErrNoFilesSpecified
	}
	changedLinks, err := changedDirectoryLinks(ctx, repoPath, skipScan)
	if err != nil {
		return err
	}
	// Git for Windows can treat a junction as a directory (and even record an
	// external repository as a gitlink). Keep named links out of git add and
	// write their link-target blobs to the temp index explicitly.
	regular := make([]string, 0, len(paths))
	linkSet := make(map[string]bool)
	for _, path := range paths {
		abs := filepath.Join(repoPath, filepath.FromSlash(path))
		st, err := dirlink.Lstat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				regular = append(regular, path) // git add records a deletion.
				continue
			}
			return camperrors.Wrapf(err, "classify path for temp index %s", path)
		}
		if !st.IsDirectoryLink() {
			regular = append(regular, path)
			continue
		}
		linkSet[cleanStageScope(path)] = true
	}
	for _, link := range scopedDirectoryLinks(changedLinks, paths) {
		linkSet[link] = true
	}
	links := make([]string, 0, len(linkSet))
	for link := range linkSet {
		links = append(links, link)
	}
	sort.Strings(links)
	if len(regular) > 0 {
		if err := WithLockRetry(ctx, repoPath, retryProfile(retry, "add-temp-index"), func() error {
			args := []string{"-C", repoPath, "add", "--"}
			for _, path := range regular {
				if literal {
					path = ":(literal)" + path
				}
				args = append(args, path)
			}
			for _, link := range changedLinks {
				args = append(args, ":(exclude,literal)"+link)
			}
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				errType := ClassifyGitError(string(out), cmd.ProcessState.ExitCode())
				if errType == GitErrorLock {
					return &LockError{Path: "index.lock", Err: err}
				}
				return camperrors.NewGit("add (temp index)", "", errType.String(), strings.TrimSpace(string(out)), err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return stageDirectoryLinks(ctx, repoPath, tmpPath, links, retry)
}
