package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// IsGitlink reports whether a path is recorded as a submodule pointer.
//
// Mode 160000 is git's gitlink. Read from HEAD rather than the index because
// the index is the user's and a background process must not depend on its
// state; a path that is a submodule in HEAD is a submodule.
func IsGitlink(ctx context.Context, repoPath, path string) bool {
	out, err := Output(ctx, repoPath, "ls-tree", "HEAD", "--", path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(out), "160000 ")
}

// CaptureGitlink snapshots the commit currently checked out at a gitlink path.
// Unlike CaptureBlobs, which refuses nested repositories, this is deliberately
// limited to a path already recorded as mode 160000 in the parent repository.
// It lets a deferred pointer follow-up retain the exact project commit even if
// the checkout moves again before the campaign-root lane reaches the job.
func CaptureGitlink(ctx context.Context, repoPath, path string) (BlobRef, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return BlobRef{}, camperrors.NewValidation("gitlink path", "must be repository-relative", nil)
	}
	if !IsGitlink(ctx, repoPath, clean) {
		return BlobRef{}, camperrors.NewValidation("gitlink path", clean+" is not a gitlink in HEAD", nil)
	}
	sha, err := FullHash(ctx, filepath.Join(repoPath, filepath.FromSlash(clean)))
	if err != nil {
		return BlobRef{}, camperrors.Wrapf(err, "capture gitlink %s", clean)
	}
	return BlobRef{Path: clean, Mode: "160000", SHA: sha}, nil
}

// BlobRef is one path's content, captured as a git object.
//
// Capturing content at enqueue is what makes a deferred bookkeeping commit mean
// what its message says. Recording paths alone defers the read as well as the
// write, so an edit made between the two lands under the earlier operation's
// message: the user renames a workitem, edits it, and the rename commit
// contains the edit.
type BlobRef struct {
	// Path is repo-relative.
	Path string
	// Mode is the git file mode ("100644", "100755", "120000", "160000").
	Mode string
	// SHA is the blob object. Empty means the path was already gone at
	// capture time and should be removed from the tree.
	SHA string
}

// CaptureBlobs writes each path's current content into the object store and
// returns references to it.
//
// A path that does not exist is captured as a deletion rather than an error,
// because camp's bookkeeping legitimately removes files (a dungeon move, a
// promoted intent) and the commit has to record that.
func CaptureBlobs(ctx context.Context, repoPath string, paths []string) ([]BlobRef, error) {
	refs := make([]BlobRef, 0, len(paths))
	for _, p := range paths {
		abs := filepath.Join(repoPath, filepath.FromSlash(p))
		info, err := os.Lstat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				deleted, captureErr := captureDeletedPath(ctx, repoPath, p)
				if captureErr != nil {
					return nil, captureErr
				}
				refs = append(refs, deleted...)
				continue
			}
			return nil, camperrors.Wrapf(err, "capture %s", p)
		}
		link, err := isCapturableDirectoryLink(abs)
		if err != nil {
			return nil, camperrors.Wrapf(err, "classify directory link %s", p)
		}
		if link {
			// A directory link's content is its target string, and `git hash-object
			// -w -- <path>` follows the link instead of reading it. For a link
			// to a file that stored the file's whole content under mode
			// 120000, so the deferred commit recorded a symlink pointing at
			// the bytes of its own target; for a link to a directory it failed
			// outright, which is how this was noticed. Read the link without
			// following into an external project tree (including junctions).
			ref, err := captureSymlink(ctx, repoPath, p)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
			continue
		}
		if info.Mode()&os.ModeIrregular != 0 {
			return nil, camperrors.Newf("capture %s: unsupported irregular entry", p)
		}
		if info.IsDir() {
			// A directory expands to its files. Capturing it as one object is
			// not possible, and a deferred job naming a directory means the
			// files under it at the moment the user acted.
			nested, err := captureDir(ctx, repoPath, p)
			if err != nil {
				return nil, err
			}
			refs = append(refs, nested...)
			continue
		}
		sha, err := Output(ctx, repoPath, "hash-object", "-w", "--", p)
		if err != nil {
			return nil, camperrors.Wrapf(err, "capture %s", p)
		}
		refs = append(refs, BlobRef{
			Path: p,
			Mode: blobMode(info),
			SHA:  strings.TrimSpace(sha),
		})
	}
	return refs, nil
}

// captureSymlink records a symlink the way git does: the blob is the link's
// target string, under mode 120000.
//
// The target is written to git hash-object over stdin rather than named as a
// path, because naming the path is exactly what makes git read through the
// link.
func isCapturableDirectoryLink(abs string) (bool, error) {
	st, err := dirlink.Lstat(abs)
	if err != nil {
		return false, err
	}
	return st.IsDirectoryLink(), nil
}

func captureSymlink(ctx context.Context, repoPath, path string) (BlobRef, error) {
	target, err := dirlink.ReadTarget(filepath.Join(repoPath, filepath.FromSlash(path)))
	if err != nil {
		return BlobRef{}, camperrors.Wrapf(err, "capture symlink %s", path)
	}
	cmd := gitCmd(ctx, repoPath, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(target)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return BlobRef{}, camperrors.Wrapf(err, "capture symlink %s: %s",
			path, strings.TrimSpace(stderr.String()))
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return BlobRef{}, camperrors.Newf("git hash-object produced no object for symlink %s", path)
	}
	return BlobRef{Path: path, Mode: "120000", SHA: sha}, nil
}

// captureDeletedPath expands a missing tracked directory into its former
// files. update-index --force-remove removes exact entries, not a directory
// prefix, so recording only the absent directory would leave every descendant
// in the deferred commit's tree.
func captureDeletedPath(ctx context.Context, repoPath, path string) ([]BlobRef, error) {
	out, err := Output(ctx, repoPath, "ls-files", "-z", "--", path)
	if err != nil {
		return nil, camperrors.Wrapf(err, "capture deleted path %s", path)
	}
	var refs []BlobRef
	for entry := range strings.SplitSeq(strings.TrimRight(out, "\x00"), "\x00") {
		if entry != "" {
			refs = append(refs, BlobRef{Path: entry})
		}
	}
	if len(refs) == 0 {
		refs = append(refs, BlobRef{Path: path})
	}
	return refs, nil
}

// captureDir captures the files under a directory that git would stage.
//
// The list comes from git rather than a filesystem walk, because a deferred
// capture has to produce the same commit the synchronous path would. A walk
// sees the disk, and the disk is not the set `git add <dir>` operates on: it
// includes ignored files, which git would refuse, and it cannot see a tracked
// file that was deleted, which git would record as a deletion. Asking
// ls-files for cached plus non-ignored untracked entries is that set exactly,
// including a file that is ignored but already tracked.
//
// It also stops at a nested repository rather than descending into it. Git
// records such a directory as a gitlink, so its files are not part of this
// repository's tree at all, and capturing them would both commit another
// project's checkout inline and produce a `<dir>/.git` entry that git rejects
// on every attempt, leaving a job that can never drain.
func captureDir(ctx context.Context, repoPath, dir string) ([]BlobRef, error) {
	if isRepoBoundary(filepath.Join(repoPath, filepath.FromSlash(dir))) {
		return nil, camperrors.Wrapf(ErrNestedRepo, "%s", dir)
	}

	out, err := Output(ctx, repoPath,
		"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", dir)
	if err != nil {
		return nil, camperrors.Wrapf(err, "capture directory %s", dir)
	}

	var paths []string
	seen := make(map[string]struct{})
	for entry := range strings.SplitSeq(strings.TrimRight(out, "\x00"), "\x00") {
		if entry == "" {
			continue
		}
		// A trailing slash is git declining to look inside: below an
		// untracked path that means an embedded repository, the same boundary
		// the check above catches when the capture starts on one.
		if nested, ok := strings.CutSuffix(entry, "/"); ok {
			return nil, camperrors.Wrapf(ErrNestedRepo, "%s", nested)
		}
		// An unmerged path is listed once per stage. Capturing it three times
		// would stage the same blob three times rather than fail, so dedupe
		// instead of rejecting.
		if _, dup := seen[entry]; dup {
			continue
		}
		seen[entry] = struct{}{}
		paths = append(paths, entry)
	}
	return CaptureBlobs(ctx, repoPath, paths)
}

// isRepoBoundary reports whether an absolute path is its own git repository.
//
// A submodule's `.git` is a file holding a gitdir pointer and an embedded
// clone's is a directory, so presence is the test and its kind is not. The
// walk root is included deliberately: `camp project add` captures the
// submodule directory itself, which is the case this exists for.
func isRepoBoundary(abs string) bool {
	_, err := os.Lstat(filepath.Join(abs, ".git"))
	return err == nil
}

// blobMode returns the git mode for a file.
func blobMode(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "120000"
	case info.Mode().Perm()&0o111 != 0:
		return "100755"
	default:
		return "100644"
	}
}

// CommitBlobs commits captured content as a scoped commit, without reading the
// working tree.
//
// The temp index starts from HEAD and receives exactly the captured objects, so
// the resulting commit is a function of what was on disk when the job was
// enqueued. Nothing here consults the real index or the current file contents.
func CommitBlobs(ctx context.Context, repoPath string, refs []BlobRef, opts *CommitOptions) error {
	if opts == nil {
		return ErrCommitOptionsRequired
	}
	if len(refs) == 0 {
		return ErrNoChanges
	}

	tmpPath, _, err := BuildTempIndexPath(repoPath)
	if err != nil {
		return err
	}
	defer RemoveTempIndex(tmpPath)

	if err := ReadTreeIntoTempIndex(ctx, repoPath, tmpPath); err != nil {
		return err
	}

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)
	for _, ref := range refs {
		var args []string
		if ref.SHA == "" {
			args = []string{"update-index", "--force-remove", "--", ref.Path}
		} else {
			args = []string{"update-index", "--add", "--cacheinfo",
				ref.Mode + "," + ref.SHA + "," + ref.Path}
		}
		if err := RunWithEnv(ctx, repoPath, env, args...); err != nil {
			return camperrors.Wrapf(err, "stage captured %s", ref.Path)
		}
	}

	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Path)
	}
	changed, err := ExpandTrackedPathsFromTempIndex(ctx, repoPath, tmpPath, paths)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return ErrNoChanges
	}

	scoped := *opts
	scoped.TempIndexPath = tmpPath
	if err := Commit(ctx, repoPath, &scoped); err != nil {
		return err
	}
	return ResetIndexToHead(ctx, repoPath, changed, opts.Retry)
}
