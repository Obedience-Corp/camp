package git

import (
	"context"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Remote represents a git remote with its fetch and push URLs.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
}

// ListRemotes returns all remotes configured in the repository at repoPath.
// Remotes are returned sorted alphabetically by name.
// Returns nil, nil if no remotes are configured.
func ListRemotes(ctx context.Context, repoPath string) ([]Remote, error) {
	output, err := RunGitCmd(ctx, repoPath, "remote", "-v")
	if err != nil {
		return nil, err
	}
	return parseRemoteVOutput(output), nil
}

// parseRemoteVOutput parses the output of `git remote -v` into Remote structs.
// Each line has the form: <name>\t<url> (fetch) or <name>\t<url> (push)
func parseRemoteVOutput(output string) []Remote {
	type entry struct {
		fetchURL string
		pushURL  string
	}
	byName := make(map[string]*entry)
	var order []string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		tabIdx := strings.Index(line, "\t")
		if tabIdx < 0 {
			continue
		}
		name := line[:tabIdx]
		rest := line[tabIdx+1:]

		var url, kind string
		if strings.HasSuffix(rest, " (fetch)") {
			url = strings.TrimSuffix(rest, " (fetch)")
			kind = "fetch"
		} else if strings.HasSuffix(rest, " (push)") {
			url = strings.TrimSuffix(rest, " (push)")
			kind = "push"
		} else {
			continue
		}

		if _, seen := byName[name]; !seen {
			byName[name] = &entry{}
			order = append(order, name)
		}
		switch kind {
		case "fetch":
			byName[name].fetchURL = url
		case "push":
			byName[name].pushURL = url
		}
	}

	if len(order) == 0 {
		return nil
	}

	sort.Strings(order)
	remotes := make([]Remote, 0, len(order))
	for _, name := range order {
		e := byName[name]
		remotes = append(remotes, Remote{
			Name:     name,
			FetchURL: e.fetchURL,
			PushURL:  e.pushURL,
		})
	}
	return remotes
}

// AddRemote adds a new remote to the repository at repoPath.
// Returns an error wrapping camperrors.ErrAlreadyExists if the remote name already exists.
func AddRemote(ctx context.Context, repoPath, name, url string) error {
	_, err := RunGitCmd(ctx, repoPath, "remote", "add", name, url)
	if err != nil {
		if HasStderr(err, "already exists") {
			return camperrors.WrapJoin(camperrors.ErrAlreadyExists, err, "add remote "+name)
		}
		return err
	}
	return nil
}

// RemoveRemote removes the named remote from the repository at repoPath.
// Returns an error wrapping camperrors.ErrNotFound if the remote does not exist.
func RemoveRemote(ctx context.Context, repoPath, name string) error {
	_, err := RunGitCmd(ctx, repoPath, "remote", "remove", name)
	if err != nil {
		if HasStderr(err, "no such remote") || HasStderr(err, "could not remove") {
			return camperrors.WrapJoin(camperrors.ErrNotFound, err, "remove remote "+name)
		}
		return err
	}
	return nil
}

// RenameRemote renames a remote from oldName to newName in the repository at repoPath.
// Returns camperrors.ErrNotFound if oldName does not exist.
// Returns camperrors.ErrAlreadyExists if newName is already taken.
func RenameRemote(ctx context.Context, repoPath, oldName, newName string) error {
	_, err := RunGitCmd(ctx, repoPath, "remote", "rename", oldName, newName)
	if err != nil {
		switch {
		case HasStderr(err, "no such remote"), HasStderr(err, "could not rename"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err, "rename remote "+oldName)
		case HasStderr(err, "already exists"):
			return camperrors.WrapJoin(camperrors.ErrAlreadyExists, err, "rename remote to "+newName)
		}
		return err
	}
	return nil
}

// SyncSubmodule syncs the submodule at subPath by running `git submodule sync`
// from repoRoot. This propagates the URL from .gitmodules into .git/config
// for the given submodule path.
func SyncSubmodule(ctx context.Context, repoRoot, subPath string) error {
	_, err := RunGitCmd(ctx, repoRoot, "submodule", "sync", "--", subPath)
	if err != nil {
		if HasStderr(err, "submodule") {
			return camperrors.WrapJoin(ErrSubmoduleSync, err, "sync submodule "+subPath)
		}
		return err
	}
	return nil
}

// SetRemoteURL updates the URL of an existing remote in the repository at repoPath.
// Returns camperrors.ErrNotFound if the remote does not exist.
func SetRemoteURL(ctx context.Context, repoPath, remoteName, url string) error {
	_, err := RunGitCmd(ctx, repoPath, "remote", "set-url", remoteName, url)
	if err != nil {
		if HasStderr(err, "no such remote") {
			return camperrors.WrapJoin(camperrors.ErrNotFound, err, "set-url remote "+remoteName)
		}
		return err
	}
	return nil
}

// VerifyRemote checks that the named remote is reachable by running
// `git ls-remote --heads`. Returns nil if the remote responds successfully.
// Returns a structured error classifying network, auth, or not-found failures.
func VerifyRemote(ctx context.Context, repoPath, remoteName string) error {
	_, err := RunGitCmd(ctx, repoPath, "ls-remote", "--heads", remoteName)
	if err != nil {
		switch {
		case HasStderr(err, "authentication failed"),
			HasStderr(err, "access denied"):
			return camperrors.WrapJoin(ErrRemoteAuthFailed, err, "verify remote "+remoteName)
		case HasStderr(err, "does not appear to be a git repository"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err, "verify remote "+remoteName)
		}
		// RunGitCmd already classifies network errors as ErrRemoteNotReachable
		return err
	}
	return nil
}

// FetchRemote fetches from the named remote in the repository at repoPath.
func FetchRemote(ctx context.Context, repoPath, remoteName string) error {
	_, err := RunGitCmd(ctx, repoPath, "fetch", remoteName)
	return err
}

// CountRemoteBranches returns the number of remote-tracking branches for the
// given remote name in the repository at repoPath.
func CountRemoteBranches(ctx context.Context, repoPath, remoteName string) (int, error) {
	output, err := RunGitCmd(ctx, repoPath, "branch", "-r", "--list", remoteName+"/*")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}
