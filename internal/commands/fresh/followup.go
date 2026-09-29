package fresh

import (
	"context"
	"errors"
	"fmt"
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

	fmt.Println()
	if dryRun {
		freshRow(fmt.Sprintf("Follow-ups (%d)", len(steps)), "preview only", ui.StatusMuted)
	} else {
		freshRow(fmt.Sprintf("Follow-ups (%d)", len(steps)), "", ui.StatusPlain)
	}

	for _, step := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if dryRun {
			freshSubRow(step.Name, "would run: "+step.Run, ui.StatusMuted)
			continue
		}

		freshSubRow(step.Name, "running", ui.StatusMuted)
		freshDetail("$ " + step.Run)

		workDir := path
		if step.Dir != "" {
			workDir = filepath.Join(path, step.Dir)
		}

		if err := runFollowUpCommand(ctx, workDir, step.Run); err != nil {
			if step.ContinueOnError {
				freshSubRow(step.Name, "failed (continuing): "+err.Error(), ui.StatusWarning)
				continue
			}
			freshSubRow(step.Name, "failed", ui.StatusError)
			return camperrors.Wrapf(err, "follow-up %q", step.Name)
		}

		freshSubRow(step.Name, "done", ui.StatusSuccess)
	}

	return nil
}

// runFollowUpCommand runs command through the shell in dir, streaming its
// stdout/stderr directly to the terminal so long-running steps (installs,
// builds) show live progress rather than a silent pause.
func runFollowUpCommand(ctx context.Context, dir, command string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return camperrors.NewCommand(command, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrapf(err, "execute %q", command)
	}

	return nil
}
