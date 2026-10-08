package jobs

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// directionBinaryNames is the lookup order for the trailer command.
//
// Current installs are fest-direction. Older shims exec direction, and that
// binary grows the same trailers subcommand.
var directionBinaryNames = []string{"fest-direction", "direction"}

// appendDirectionTrailers runs `fest-direction trailers --tree` when this
// repository's only commit hook is that shim.
//
// commit-tree does not run hooks. The shim's effect is trailers of the tree
// the job already captured, so the worker appends them from job.Tree and uses
// the command's stdout as the message. A repository with no commit hooks returns
// message unchanged and does not look for the binary: there is nothing to
// reproduce. Other hooks or inspection errors fail the job, including hooks
// installed after capture. A missing binary or a non-zero exit also fails. The commit
// must not land without the trailers the hook would have added, and it must
// not land when the hook would have refused.
func appendDirectionTrailers(ctx context.Context, repoPath string, job *Job, message string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	shim, err := git.OnlyDirectionShim(ctx, repoPath)
	if err != nil {
		return "", camperrors.Wrapf(err, "job %s: cannot defer commit", job.ID)
	}
	if !shim {
		return message, nil
	}
	bin, err := lookupDirectionBinary()
	if err != nil {
		return "", camperrors.Wrapf(err, "job %s", job.ID)
	}
	cmd := exec.CommandContext(ctx, bin, "trailers", "--tree", job.Tree)
	// The command reads the work unit from the repository it is run in
	// (.direction/config.yaml), not from the index. The caller's cwd is not
	// that repository: a worker has none that matters.
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
