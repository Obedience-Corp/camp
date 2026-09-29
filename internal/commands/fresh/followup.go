package fresh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// resolveFreshFollowUps resolves the follow-up steps for a project, honoring
// --no-follow-up by resolving to no steps regardless of configuration.
func resolveFreshFollowUps(cfg *config.FreshConfig, projectName string, noFollowUp bool) []config.FollowUpConfig {
	if noFollowUp {
		return nil
	}
	return cfg.ResolveFreshFollowUps(projectName)
}

// runFreshFollowUps runs the configured follow-up command steps, in order,
// after a successful sync/prune/branch cycle. On dry-run each step is listed
// but never executed. A step that fails without continue_on_error aborts the
// remaining steps and the fresh cycle for this project.
func runFreshFollowUps(ctx context.Context, path string, steps []config.FollowUpConfig, dryRun bool) error {
	if len(steps) == 0 {
		return nil
	}

	note := fmt.Sprintf("(%d)", len(steps))
	if dryRun {
		note = "preview only"
	}
	freshHeading("Follow-ups", note)

	for _, step := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if dryRun {
			freshRow(step.Name, "would run", ui.StatusMuted)
			freshDetail(step.Run)
			continue
		}

		workDir := path
		if step.Dir != "" {
			workDir = filepath.Join(path, step.Dir)
		}
		stepLive := newFreshLive(os.Stdout, step.Name, "running", "$ "+step.Run)
		stepLive.Begin()
		if err := runFollowUpCommand(ctx, workDir, step.Run, stepLive.Stream()); err != nil {
			if step.ContinueOnError {
				stepLive.Finish("failed (continuing): "+err.Error(), ui.StatusWarning)
				continue
			}
			stepLive.Finish("failed", ui.StatusError)
			return camperrors.Wrapf(err, "follow-up %q", step.Name)
		}
		stepLive.Finish("done", ui.StatusSuccess)
	}

	return nil
}

// runFollowUpCommand runs command through the shell in dir, streaming its
// stdout/stderr directly to the terminal so long-running steps (installs,
// builds) show live progress rather than a silent pause.
func runFollowUpCommand(ctx context.Context, dir, command string, output io.Writer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return camperrors.NewCommand(command, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrapf(err, "execute %q", command)
	}

	return nil
}
