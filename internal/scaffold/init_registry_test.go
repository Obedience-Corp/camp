package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_RegistryFailurePropagates(t *testing.T) {
	root := t.TempDir()
	registryDir := filepath.Join(root, "registry")
	if err := os.Mkdir(registryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(registryDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(registryDir, 0o755) })
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(registryDir, "registry.json"))

	_, err := Init(context.Background(), filepath.Join(root, "campaign"), InitOptions{
		Name:        "registry-fail",
		SkipGitInit: true,
	})
	if err == nil {
		t.Fatal("Init() expected registry failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to register campaign") {
		t.Fatalf("Init() error missing registry context: %v", err)
	}
}

// With no home directory and no override, the registry has nowhere to live.
// Init must say so instead of writing ./.obey/campaign/registry.json, which is
// what made a created campaign vanish from `camp list` run anywhere else.
func TestInit_NoHomeFailsRatherThanRegisteringRelative(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CAMP_REGISTRY_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	_, err := Init(context.Background(), filepath.Join(root, "campaign"), InitOptions{
		Name:        "no-home",
		SkipGitInit: true,
	})
	if err == nil {
		t.Fatal("Init() expected an error with no resolvable registry location, got nil")
	}
	// Init reaches global config before the registry, so either resolver may be
	// the one that reports it; what matters is that the operator is told the
	// home directory is the problem instead of getting a silent relative write.
	if !strings.Contains(err.Error(), "cannot determine home directory") {
		t.Fatalf("Init() error does not name the unresolvable home directory: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".obey")); !os.IsNotExist(statErr) {
		t.Errorf("Init() created a working-directory-relative .obey (stat err = %v)", statErr)
	}
}
