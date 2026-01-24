package fest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	festPath     string
	festPathOnce sync.Once
	festPathErr  error
)

// FindFestCLI locates the fest CLI binary.
// Returns the full path or error if not found.
// Results are cached for efficiency.
func FindFestCLI() (string, error) {
	festPathOnce.Do(func() {
		festPath, festPathErr = findFestCLI()
	})
	return festPath, festPathErr
}

// findFestCLI performs the actual lookup.
func findFestCLI() (string, error) {
	// Check PATH first (most common case)
	if path, err := exec.LookPath("fest"); err == nil {
		return path, nil
	}

	// Check common installation locations
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, "go", "bin", "fest"),
		filepath.Join(home, ".local", "bin", "fest"),
		"/usr/local/bin/fest",
		"/opt/homebrew/bin/fest", // macOS homebrew
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}

	return "", ErrFestNotFound
}

// IsFestAvailable returns true if fest CLI is installed.
func IsFestAvailable() bool {
	_, err := FindFestCLI()
	return err == nil
}

// ResetCache clears the cached fest path for testing purposes.
func ResetCache() {
	festPathOnce = sync.Once{}
	festPath = ""
	festPathErr = nil
}

// FestInfo contains information about the fest installation.
type FestInfo struct {
	Path    string
	Version string
}

// GetFestVersion runs fest --version and returns the version string.
func GetFestVersion(ctx context.Context, festPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, festPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get fest version: %w", err)
	}

	// Parse version from output (typically "fest version X.Y.Z" or just "X.Y.Z")
	version := strings.TrimSpace(string(output))
	return version, nil
}

// VerifyFest checks that fest is installed and working.
func VerifyFest(ctx context.Context) (*FestInfo, error) {
	path, err := FindFestCLI()
	if err != nil {
		return nil, err
	}

	version, err := GetFestVersion(ctx, path)
	if err != nil {
		return nil, err
	}

	return &FestInfo{
		Path:    path,
		Version: version,
	}, nil
}
