package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// changedDirectoryLinks collapses paths Git reports beneath a directory link
// to the link entry itself. Windows Git may report a junction as a directory,
// including files in its external target, so callers must exclude these roots
// from git add before staging their link blobs explicitly.
//
// skip are directories the scan leaves out, such as declared artifact roots,
// whose links the root plan finds instead.
func changedDirectoryLinks(ctx context.Context, repoPath string, skip []string) ([]string, error) {
	args := []string{"-uall"}
	if len(skip) > 0 {
		args = append(args, "--", ".")
		for _, dir := range skip {
			args = append(args, ":(exclude,literal)"+strings.TrimSuffix(dir, "/"))
		}
	}
	out, err := StatusPorcelain(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, entry := range ParseStatusPorcelainZ(out) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		link, err := dirlink.FindAncestor(repoPath, entry.Path)
		if err != nil {
			return nil, camperrors.Wrapf(err, "classify changed path %s", entry.Path)
		}
		if link != "" {
			seen[link] = true
		}
	}
	links := make([]string, 0, len(seen))
	for link := range seen {
		links = append(links, link)
	}
	sort.Strings(links)
	return links, nil
}

func directoryLinkSizes(ctx context.Context, repoPath string, links []string) (map[string]int64, error) {
	sizes := make(map[string]int64, len(links))
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target, err := dirlink.ReadTarget(filepath.Join(repoPath, filepath.FromSlash(link)))
		if err != nil {
			return nil, camperrors.Wrapf(err, "read directory link %s for staging guard", link)
		}
		sizes[link] = int64(len(target))
	}
	return sizes, nil
}

// scopedDirectoryLinks chooses links named by a git-add scope. The positive
// scopes used by Camp are literal paths or "."; exclusions come from the
// staging guard and declared submodule paths.
func scopedDirectoryLinks(links, files []string) []string {
	if len(files) == 0 {
		return links
	}
	var included, excluded []string
	for _, file := range files {
		if file == "--" {
			continue
		}
		if path, ok := strings.CutPrefix(file, ":(exclude,literal)"); ok {
			excluded = append(excluded, cleanStageScope(path))
			continue
		}
		included = append(included, cleanStageScope(file))
	}
	var selected []string
	for _, link := range links {
		match := false
		for _, scope := range included {
			if scope == "." || link == scope || strings.HasPrefix(link, scope+"/") {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		for _, scope := range excluded {
			if scope == "." || link == scope || strings.HasPrefix(link, scope+"/") {
				match = false
				break
			}
		}
		if match {
			selected = append(selected, link)
		}
	}
	return selected
}

func cleanStageScope(path string) string {
	if path == "." {
		return path
	}
	return strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), "/")
}

func excludeDirectoryLinks(files, links []string) []string {
	if len(links) == 0 {
		return files
	}
	if len(files) == 0 {
		files = []string{"--", "."}
	} else {
		files = append([]string(nil), files...)
	}
	for _, link := range links {
		files = append(files, ":(exclude,literal)"+link)
	}
	return files
}

func stageDirectoryLinks(ctx context.Context, repoPath, indexPath string, links []string, retry *RetryConfig) error {
	if len(links) == 0 {
		return nil
	}
	for _, link := range links {
		// ReadTarget fails if the link was replaced by an ordinary directory
		// after discovery. CaptureBlobs would recurse in that race.
		ref, err := captureSymlink(ctx, repoPath, link)
		if err != nil {
			return camperrors.Wrapf(err, "capture directory link %s", link)
		}
		if err := WithLockRetry(ctx, repoPath, retryProfile(retry, "add-directory-link"), func() error {
			args := []string{"-C", repoPath, "update-index", "--add", "--cacheinfo", ref.Mode + "," + ref.SHA + "," + ref.Path}
			cmd := exec.CommandContext(ctx, "git", args...)
			if indexPath != "" {
				cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				errType := ClassifyGitError(string(out), cmd.ProcessState.ExitCode())
				if errType == GitErrorLock {
					return &LockError{Path: "index.lock", Err: err}
				}
				return camperrors.NewGit("update-index", "", errType.String(), strings.TrimSpace(string(out)), err)
			}
			return nil
		}); err != nil {
			return camperrors.Wrapf(err, "stage directory link %s", ref.Path)
		}
	}
	return nil
}
