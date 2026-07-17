package sync

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// PRState is the lifecycle state gh reports for a pull request.
type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateClosed PRState = "CLOSED"
	PRStateMerged PRState = "MERGED"
)

// Sentinel errors for the gh boundary.
var (
	// ErrGHNotFound indicates the gh CLI is not on PATH.
	ErrGHNotFound = errors.New("gh CLI not found in PATH")

	// ErrGHQueryFailed indicates gh ran but the PR state could not be resolved.
	ErrGHQueryFailed = errors.New("gh pr view failed")
)

// PRChecker resolves the current state of a GitHub pull request from its URL.
// The real implementation shells out to gh; tests inject a fake so the sync
// decision table (Plan) never touches a subprocess.
type PRChecker interface {
	State(ctx context.Context, url string) (PRState, error)
}

// GHChecker is the real PRChecker, backed by the gh CLI.
type GHChecker struct {
	ghPath string
}

// NewGHChecker locates gh on PATH. It returns ErrGHNotFound (wrapped with
// install guidance) when gh is missing, so camp intent sync can fail with a
// clear, actionable message rather than an opaque exec error.
func NewGHChecker() (*GHChecker, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return nil, camperrors.Wrap(ErrGHNotFound, "install it from https://cli.github.com and run 'gh auth login'")
	}
	return &GHChecker{ghPath: path}, nil
}

type ghPRView struct {
	State string `json:"state"`
}

// State runs `gh pr view <url> --json state` and parses the result.
func (c *GHChecker) State(ctx context.Context, url string) (PRState, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}

	cmd := exec.CommandContext(ctx, c.ghPath, "pr", "view", url, "--json", "state")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		stderr := ""
		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", camperrors.Wrapf(ErrGHQueryFailed, "%s: %s", url, stderr)
	}

	var parsed ghPRView
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", camperrors.Wrapf(ErrGHQueryFailed, "parsing gh output for %s: %v", url, err)
	}
	return PRState(parsed.State), nil
}
