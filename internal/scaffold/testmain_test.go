package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	testRoot, err := os.MkdirTemp("", "camp-scaffold-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	homeDir := filepath.Join(testRoot, "home")
	xdgDir := filepath.Join(testRoot, "xdg")
	registryPath := filepath.Join(testRoot, "registry.json")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}

	// Keep scaffold tests off the operator's real config and registry even when
	// a test forgets to override registration or XDG paths explicitly.
	if err := os.Setenv("HOME", homeDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", xdgDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	if err := os.Setenv("CAMP_REGISTRY_PATH", registryPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(testRoot)
	os.Exit(code)
}
