package git

import (
	"context"
	"errors"
	"os/exec"
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
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "-v")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			errType := ClassifyGitError(stderr, exitErr.ExitCode())
			if errType == GitErrorNotRepo {
				return nil, camperrors.WrapJoin(ErrNotRepository, err, "list remotes")
			}
		}
		return nil, camperrors.Wrapf(err, "list remotes in %s", repoPath)
	}

	return parseRemoteVOutput(string(output)), nil
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
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "add", name, url)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		lower := strings.ToLower(stderr)
		switch {
		case strings.Contains(lower, "already exists"):
			return camperrors.WrapJoin(camperrors.ErrAlreadyExists, err,
				"add remote "+name)
		case strings.Contains(lower, "not a git repository"):
			return camperrors.WrapJoin(ErrNotRepository, err,
				"add remote "+name)
		}
		return camperrors.Wrapf(err, "add remote %s in %s: %s", name, repoPath, stderr)
	}
	return nil
}

// RemoveRemote removes the named remote from the repository at repoPath.
// Returns an error wrapping camperrors.ErrNotFound if the remote does not exist.
func RemoveRemote(ctx context.Context, repoPath, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "remove", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		lower := strings.ToLower(stderr)
		switch {
		case strings.Contains(lower, "no such remote"),
			strings.Contains(lower, "could not remove"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err,
				"remove remote "+name)
		case strings.Contains(lower, "not a git repository"):
			return camperrors.WrapJoin(ErrNotRepository, err,
				"remove remote "+name)
		}
		return camperrors.Wrapf(err, "remove remote %s in %s: %s", name, repoPath, stderr)
	}
	return nil
}

// RenameRemote renames a remote from oldName to newName in the repository at repoPath.
// Returns camperrors.ErrNotFound if oldName does not exist.
// Returns camperrors.ErrAlreadyExists if newName is already taken.
func RenameRemote(ctx context.Context, repoPath, oldName, newName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "rename", oldName, newName)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		lower := strings.ToLower(stderr)
		switch {
		case strings.Contains(lower, "no such remote"),
			strings.Contains(lower, "could not rename"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err,
				"rename remote "+oldName)
		case strings.Contains(lower, "already exists"):
			return camperrors.WrapJoin(camperrors.ErrAlreadyExists, err,
				"rename remote to "+newName)
		case strings.Contains(lower, "not a git repository"):
			return camperrors.WrapJoin(ErrNotRepository, err,
				"rename remote in "+repoPath)
		}
		return camperrors.Wrapf(err, "rename remote %s to %s in %s: %s",
			oldName, newName, repoPath, stderr)
	}
	return nil
}

// SyncSubmodule syncs the submodule at subPath by running `git submodule sync`
// from repoRoot. This propagates the URL from .gitmodules into .git/config
// for the given submodule path.
func SyncSubmodule(ctx context.Context, repoRoot, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "submodule", "sync", "--", subPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		return camperrors.WrapJoin(ErrSubmoduleSync, err,
			"sync submodule "+subPath+": "+stderr)
	}
	return nil
}

// SetRemoteURL updates the URL of an existing remote in the repository at repoPath.
// Returns camperrors.ErrNotFound if the remote does not exist.
func SetRemoteURL(ctx context.Context, repoPath, remoteName, url string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "set-url", remoteName, url)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		lower := strings.ToLower(stderr)
		switch {
		case strings.Contains(lower, "no such remote"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err,
				"set-url remote "+remoteName)
		case strings.Contains(lower, "not a git repository"):
			return camperrors.WrapJoin(ErrNotRepository, err,
				"set-url remote "+remoteName)
		}
		return camperrors.Wrapf(err, "set remote %s url in %s: %s", remoteName, repoPath, stderr)
	}
	return nil
}

// VerifyRemote checks that the named remote is reachable by running
// `git ls-remote --heads`. Returns nil if the remote responds successfully.
// Returns a structured error classifying network, auth, or not-found failures.
func VerifyRemote(ctx context.Context, repoPath, remoteName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-remote", "--heads", remoteName)
	if output, err := cmd.CombinedOutput(); err != nil {
		stderr := strings.TrimSpace(string(output))
		lower := strings.ToLower(stderr)
		switch {
		case strings.Contains(lower, "could not resolve host"),
			strings.Contains(lower, "connection refused"),
			strings.Contains(lower, "failed to connect"),
			strings.Contains(lower, "connection timed out"):
			return camperrors.WrapJoin(ErrRemoteNotReachable, err,
				"verify remote "+remoteName+": "+stderr)
		case strings.Contains(lower, "authentication failed"),
			strings.Contains(lower, "access denied"),
			strings.Contains(lower, "permission denied"):
			return camperrors.WrapJoin(ErrRemoteAuthFailed, err,
				"verify remote "+remoteName+": "+stderr)
		case strings.Contains(lower, "no such remote"),
			strings.Contains(lower, "does not appear to be a git repository"):
			return camperrors.WrapJoin(camperrors.ErrNotFound, err,
				"verify remote "+remoteName)
		}
		return camperrors.Wrapf(err, "verify remote %s in %s: %s", remoteName, repoPath, stderr)
	}
	return nil
}
