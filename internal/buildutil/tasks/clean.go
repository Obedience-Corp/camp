// internal/buildutil/tasks/clean.go
package tasks

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/buildutil/ui"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const campModulePath = "module github.com/Obedience-Corp/camp"

// Clean removes build artifacts
func Clean(verbose bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "clean: determine cwd")
	}
	repoRoot, err := findCampRepoRoot(cwd)
	if err != nil {
		return err
	}

	ui.Section("Cleaning Build Artifacts")

	artifacts := []string{
		"bin/",
		"*.test",
		"*.exe",
		"coverage.out",
		"coverage_*.out",
		".test-*",
		"*.disabled",
		"*.old",
		"*.wip",
		"*.backup",
		"*.tmp",
		"*.bak",
		"*~",
		".test-results.tmp",
		".test-timing.tmp",
		"coverage.html",
	}

	total := len(artifacts)
	removed := 0

	for i, pattern := range artifacts {
		ui.Progress(i+1, total, fmt.Sprintf("Removing %s", pattern))

		if strings.Contains(pattern, "*") {
			matches, err := filepath.Glob(filepath.Join(repoRoot, pattern))
			if err != nil {
				return camperrors.Wrapf(err, "clean glob %s", pattern)
			}
			for _, match := range matches {
				if err := os.RemoveAll(match); err != nil {
					return camperrors.Wrapf(err, "remove %s", match)
				}
				removed++
			}
		} else {
			// Direct removal for specific files/directories
			target := filepath.Join(repoRoot, pattern)
			if err := os.RemoveAll(target); err != nil {
				return camperrors.Wrapf(err, "remove %s", target)
			}
			removed++
		}

		time.Sleep(50 * time.Millisecond) // Small delay for visual effect
	}

	ui.ClearProgress()

	// Also clean up any .test binaries in subdirectories
	if err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip vendor and .git directories
		if info.IsDir() && (info.Name() == "vendor" || info.Name() == ".git") {
			return filepath.SkipDir
		}

		// Remove .test files
		if strings.HasSuffix(info.Name(), ".test") {
			os.Remove(path)
			removed++
		}

		return nil
	}); err != nil {
		return camperrors.Wrap(err, "walk repo for test binaries")
	}

	// Clean up orphaned test containers (testcontainers)
	ui.Task("Cleaning", "orphaned Docker test containers")
	dockerCmd := exec.Command("docker", "container", "prune", "-f", "--filter", "label=org.testcontainers=true")
	if verbose {
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr
	}
	if err := dockerCmd.Run(); err != nil {
		// Docker might not be available, that's OK
		if verbose {
			fmt.Printf("Note: Docker cleanup skipped (docker not available)\n")
		}
	}
	ui.TaskPass()

	// Display summary
	removeStatus := fmt.Sprintf("✓ %d items removed", removed)
	cleanStatus := "✓ Complete"

	if ui.ColourEnabled() {
		removeStatus = ui.Green + removeStatus + ui.Reset
		cleanStatus = ui.Green + cleanStatus + ui.Reset
	}

	rows := [][]string{
		{"Action", "Status"},
		{"Remove build artifacts", removeStatus},
		{"Clean workspace", cleanStatus},
	}

	ui.SummaryCardWithStatus("Clean Summary", rows, "< 1s", true, "✓ CLEAN SUCCESSFUL", "✗ CLEAN FAILED")

	return nil
}

func findCampRepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		goModPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil {
			if bytes.Contains(data, []byte(campModulePath)) {
				return dir, nil
			}
		} else if !os.IsNotExist(err) {
			return "", camperrors.Wrapf(err, "read %s", goModPath)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", camperrors.Wrapf(camperrors.ErrNotFound, "clean: cannot find camp repo root from cwd %s", start)
}
