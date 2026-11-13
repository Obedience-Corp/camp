package git

import (
	"bytes"
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// RunWithEnv runs a git command with additional environment variables.
//
// It exists for GIT_INDEX_FILE: the deferred-commit worker materializes a
// captured tree into a scratch index so the message writer sees the snapshot
// being committed rather than the live one. Everything else in this package
// deliberately does not take an environment, because a caller that can set
// arbitrary git variables can also defeat the guarantees the rest of it makes.
func RunWithEnv(ctx context.Context, repoPath string, env []string, args ...string) error {
	cmd := gitCmd(ctx, repoPath, args...)
	cmd.Env = gitEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return camperrors.Wrapf(err, "git %s: %s",
			strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return nil
}

// OutputWithEnv is RunWithEnv for commands whose stdout is the result, such as
// `git write-tree` against a scratch index.
func OutputWithEnv(ctx context.Context, repoPath string, env []string, args ...string) (string, error) {
	cmd := gitCmd(ctx, repoPath, args...)
	cmd.Env = gitEnv(env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", camperrors.Wrapf(err, "git %s: %s",
			strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
