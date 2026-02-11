package leverage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// SCCRunner implements Runner by shelling out to the scc binary.
type SCCRunner struct {
	binaryPath string
	cocomoType string
}

// NewSCCRunner creates an SCCRunner after verifying scc is installed.
// cocomoType controls the COCOMO model variant passed to scc (e.g. "organic").
// Returns an error with installation instructions if scc is not in PATH.
func NewSCCRunner(cocomoType string) (*SCCRunner, error) {
	path, err := exec.LookPath("scc")
	if err != nil {
		return nil, fmt.Errorf("scc not found: install with 'brew install scc' or visit https://github.com/boyter/scc")
	}
	return &SCCRunner{binaryPath: path, cocomoType: cocomoType}, nil
}

// Run executes scc on dir and returns the parsed json2 result.
// excludeDirs specifies subdirectory names to skip (passed as --exclude-dir flags).
func (r *SCCRunner) Run(ctx context.Context, dir string, excludeDirs []string) (*SCCResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	args := []string{
		"--format", FormatJSON2,
		"--cocomo-project-type", r.cocomoType,
	}
	for _, d := range excludeDirs {
		args = append(args, "--exclude-dir", d)
	}
	args = append(args, dir)

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("scc cancelled: %w", ctx.Err())
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("scc failed on %s: %w\nstderr: %s", dir, err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("scc failed on %s: %w", dir, err)
	}

	var result SCCResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse scc json2 output: %w", err)
	}

	return &result, nil
}
