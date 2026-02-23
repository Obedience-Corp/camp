package leverage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// DefaultExcludeDirs are directories excluded from scc scans by default.
// These contain vendored dependencies, build output, or caches that
// would inflate COCOMO estimates beyond authored code.
var DefaultExcludeDirs = []string{
	"node_modules",
	"vendor",
	".venv",
	"venv",
	"dist",
	"build",
	"target",
	"__pycache__",
	".next",
	".nuxt",
	"bower_components",
	"Pods",
	".tox",
	".eggs",
	".mypy_cache",
	".pytest_cache",
	".cargo",
}

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

	merged := mergeExcludeDirs(DefaultExcludeDirs, excludeDirs)

	args := []string{
		"--format", FormatJSON2,
		"--cocomo-project-type", r.cocomoType,
	}
	for _, d := range merged {
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

// mergeExcludeDirs combines default and extra exclude dirs, removing duplicates.
func mergeExcludeDirs(defaults, extras []string) []string {
	seen := make(map[string]struct{}, len(defaults)+len(extras))
	result := make([]string, 0, len(defaults)+len(extras))
	for _, d := range defaults {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			result = append(result, d)
		}
	}
	for _, d := range extras {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			result = append(result, d)
		}
	}
	return result
}
