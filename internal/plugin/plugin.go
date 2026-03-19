// Package plugin provides git-style plugin discovery and execution for camp.
// Any binary named camp-<name> on PATH becomes invocable as "camp <name> [args...]".
package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Prefix is the naming convention for camp plugin binaries.
const Prefix = "camp-"

// EnvCampRoot is the environment variable set for plugin processes.
const EnvCampRoot = "CAMP_ROOT"

// Plugin represents a discovered camp plugin binary.
type Plugin struct {
	// Name is the subcommand name (e.g., "graph" for camp-graph).
	Name string
	// Path is the absolute path to the plugin binary.
	Path string
}

// Lookup checks if a plugin binary exists for the given subcommand name.
// It searches PATH for "camp-<name>" using exec.LookPath.
func Lookup(name string) (Plugin, bool) {
	binName := Prefix + name
	path, err := exec.LookPath(binName)
	if err != nil {
		return Plugin{}, false
	}
	return Plugin{Name: name, Path: path}, true
}

// Discover scans PATH for all camp plugin binaries and returns them.
// The first match per name wins (matching git semantics).
func Discover(ctx context.Context) ([]Plugin, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	var plugins []Plugin

	for _, dir := range filepath.SplitList(pathEnv) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			if !strings.HasPrefix(name, Prefix) {
				continue
			}

			// On Windows, strip known extensions
			subcommand := strings.TrimPrefix(name, Prefix)
			if runtime.GOOS == "windows" {
				subcommand = strings.TrimSuffix(subcommand, ".exe")
				subcommand = strings.TrimSuffix(subcommand, ".cmd")
				subcommand = strings.TrimSuffix(subcommand, ".bat")
			}

			if subcommand == "" || seen[subcommand] {
				continue
			}

			// Verify the file is executable
			fullPath := filepath.Join(dir, name)
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}

			seen[subcommand] = true
			plugins = append(plugins, Plugin{Name: subcommand, Path: fullPath})
		}
	}

	return plugins, nil
}

// Execute runs a plugin binary with the given arguments.
// It connects stdin/stdout/stderr and sets CAMP_ROOT if campRoot is non-empty.
// Returns a CommandError on non-zero exit code.
func Execute(ctx context.Context, p Plugin, args []string, campRoot string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, p.Path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	if campRoot != "" {
		env = append(env, EnvCampRoot+"="+campRoot)
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return camperrors.NewCommand(Prefix+p.Name, exitErr.ExitCode(), "", exitErr)
		}
		return camperrors.Wrap(err, "failed to execute plugin "+p.Name)
	}

	return nil
}
