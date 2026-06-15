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
