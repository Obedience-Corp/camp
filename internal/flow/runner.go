package flow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/shellcmd"
)

// Runner executes workflow flows with proper context handling,
// working directory resolution, and environment setup.
type Runner struct {
	campaignRoot string
}

// NewRunner creates a new flow runner for the given campaign root.
func NewRunner(campaignRoot string) *Runner {
	return &Runner{campaignRoot: campaignRoot}
}

// Run executes a flow with the given context and optional extra arguments.
// Commands use the flow's shell with inherited stdio.
func (r *Runner) Run(ctx context.Context, f Flow, extraArgs []string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Newf("context cancelled: %w", err)
	}

	workDir := r.resolveWorkDir(f.WorkDir)

	cmd, err := shellcmd.CommandInDir(ctx, f.Shell, f.Command, extraArgs, workDir)
	if err != nil {
		return camperrors.Wrap(err, "resolving flow interpreter")
	}
	cmd.Env = r.mergeEnv(f.Env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return camperrors.Newf("flow command cancelled: %w", errors.Join(ctx.Err(), err))
		}
		return camperrors.Newf("executing flow command: %w", err)
	}

	return nil
}

// resolveWorkDir resolves the flow's working directory relative to campaign root.
func (r *Runner) resolveWorkDir(workDir string) string {
	if workDir == "" || workDir == "." {
		return r.campaignRoot
	}
	if filepath.IsAbs(workDir) {
		return workDir
	}
	return filepath.Join(r.campaignRoot, workDir)
}

// mergeEnv merges the flow's environment with the process environment.
func (r *Runner) mergeEnv(flowEnv map[string]string) []string {
	env := os.Environ()

	if len(flowEnv) == 0 {
		return env
	}

	envMap := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	for key, value := range flowEnv {
		envMap[key] = value
	}

	result := make([]string, 0, len(envMap))
	for key, value := range envMap {
		result = append(result, key+"="+value)
	}

	return result
}
