package flow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// Commands are executed via "sh -c" with inherited stdio.
func (r *Runner) Run(ctx context.Context, f Flow, extraArgs []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	workDir := r.resolveWorkDir(f.WorkDir)

	command := f.Command
	if len(extraArgs) > 0 {
		command = command + " " + strings.Join(extraArgs, " ")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = r.mergeEnv(f.Env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("executing flow command: %w", err)
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
