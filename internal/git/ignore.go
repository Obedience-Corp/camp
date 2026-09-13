package git

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// FilterIgnored returns paths with every entry matched by the repository's
// ignore rules removed, preserving order. Naming an ignored path to git add
// is a hard error rather than a skip, so a caller staging a known file list
// (a scaffold, a repair) has to drop those entries before it stages.
func FilterIgnored(ctx context.Context, repoPath string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// Exit status 1 is check-ignore's "nothing matched", not a failure.
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return nil, camperrors.NewGit("check-ignore", "", "", strings.TrimSpace(stderr.String()), err)
		}
		return paths, nil
	}
	ignored := make(map[string]struct{})
	for _, p := range strings.Split(strings.TrimRight(stdout.String(), "\x00"), "\x00") {
		if p != "" {
			ignored[p] = struct{}{}
		}
	}
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, skip := ignored[p]; !skip {
			kept = append(kept, p)
		}
	}
	return kept, nil
}
