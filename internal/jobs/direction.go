package jobs

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// directionBinaryNames is the lookup order for the trailer command.
//
// Current installs are fest-direction. Older shims exec direction, and that
// binary grows the same trailers subcommand.
var directionBinaryNames = []string{"fest-direction", "direction"}

// DirectionContext is the resolved enqueue-time hook context. A non-nil
// context with an empty WorkUnit records an intentionally unconfigured hook.
type DirectionContext struct {
	WorkUnit string `json:"work_unit"`
}

// CaptureDirectionContext follows the direction hook's precedence: the
// DIRECTION_WORK_UNIT environment variable, then default_work_unit in
// .direction/config.yaml. Resolve before queueing, while this invocation's
// environment and working tree still describe the captured commit.
func CaptureDirectionContext(ctx context.Context, repoPath, tree string) (*DirectionContext, error) {
	shim, err := git.OnlyDirectionShim(ctx, repoPath)
	if err != nil || !shim {
		return nil, err
	}
	candidate := os.Getenv("DIRECTION_WORK_UNIT")
	if candidate == "" {
		body, err := os.ReadFile(filepath.Join(repoPath, ".direction", "config.yaml"))
		if err != nil && !os.IsNotExist(err) {
			return nil, camperrors.Wrap(err, "read direction context")
		}
		var config struct {
			DefaultWorkUnit string `yaml:"default_work_unit"`
		}
		if err == nil {
			if err := yaml.Unmarshal(body, &config); err != nil {
				return nil, camperrors.Wrap(err, "parse direction context")
			}
		}
		candidate = config.DefaultWorkUnit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if candidate == "" {
		return &DirectionContext{}, nil
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve direction repository")
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		candidate = resolved
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve direction work unit")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, camperrors.New("direction work unit is outside the repository")
	}
	rel = filepath.ToSlash(rel)
	object := tree
	if rel != "." {
		object += ":" + rel
	}
	// The direction binary can infer a renamed unit from live HEAD. A queued
	// job cannot rely on that mutable history: leave rename repair foreground.
	kind, err := git.Output(ctx, repoPath, "cat-file", "-t", object)
	if err != nil {
		return nil, camperrors.Wrap(err, "direction work unit is not in the captured tree; commit in the foreground")
	}
	if kind != "tree" {
		return nil, camperrors.New("direction work unit is not a directory in the captured tree")
	}
	return &DirectionContext{WorkUnit: rel}, nil
}

// appendDirectionTrailers reproduces the captured hook context against job.Tree.
// Live custom hooks or inspection failures refuse the job. An old job with no
// captured context cannot guess what a direction hook meant at enqueue time.
func appendDirectionTrailers(ctx context.Context, repoPath string, job *Job, message string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	shim, err := git.OnlyDirectionShim(ctx, repoPath)
	if err != nil {
		return "", camperrors.Wrapf(err, "job %s: cannot defer commit", job.ID)
	}
	if job.Direction == nil {
		if shim {
			return "", camperrors.Newf("job %s: direction context was not captured; commit in the foreground", job.ID)
		}
		return message, nil
	}
	if job.Direction.WorkUnit == "" {
		return message, nil
	}
	bin, err := lookupDirectionBinary()
	if err != nil {
		return "", camperrors.Wrapf(err, "job %s", job.ID)
	}
	cmd := exec.CommandContext(ctx, bin, "trailers", "--tree", job.Tree, "--work-unit", job.Direction.WorkUnit)
	// An explicit work unit overrides both live config and worker environment.
	cmd.Dir = repoPath
	cmd.Stdin = strings.NewReader(message)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", camperrors.Wrapf(err, "job %s: direction trailers: %s", job.ID, detail)
		}
		return "", camperrors.Wrapf(err, "job %s: direction trailers", job.ID)
	}
	// Stdout is the message, trailers included, or the message unchanged when
	// no work unit is configured. Empty output would replace that message
	// with nothing.
	out := stdout.String()
	if out == "" {
		return "", camperrors.Newf("job %s: direction trailers produced an empty message", job.ID)
	}
	return out, nil
}

func lookupDirectionBinary() (string, error) {
	for _, name := range directionBinaryNames {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", camperrors.New("neither fest-direction nor direction is on PATH")
}
