package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

func TestShortenRemoteURL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://github.com/Obedience-Corp/camp.git", "Obedience-Corp/camp"},
		{"https://github.com/Obedience-Corp/camp", "Obedience-Corp/camp"},
		{"git@github.com:Obedience-Corp/camp.git", "Obedience-Corp/camp"},
		{"git@github.com:Obedience-Corp/camp", "Obedience-Corp/camp"},
		{"https://gitlab.com/org/repo.git", "https://gitlab.com/org/repo"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortenRemoteURL(tt.input); got != tt.want {
			t.Errorf("shortenRemoteURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStatusAll_JSON_NoCache(t *testing.T) {
	root := setupStatusAllTestCampaign(t)
	t.Setenv(campaign.EnvCampaignRoot, root)
	campaign.ClearCache()
	t.Cleanup(campaign.ClearCache)

	oldJSON := statusAllJSON
	oldView := statusAllView
	oldNoRecurse := statusAllNoRecurse
	oldRemoteURL := statusAllRemoteURL
	t.Cleanup(func() {
		statusAllJSON = oldJSON
		statusAllView = oldView
		statusAllNoRecurse = oldNoRecurse
		statusAllRemoteURL = oldRemoteURL
	})
	statusAllJSON = true
	statusAllView = false
	statusAllNoRecurse = false
	statusAllRemoteURL = false

	stdout, err := captureStatusAllStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		return runStatusAll(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runStatusAll() error = %v", err)
	}

	var statuses []repoStatus
	if err := json.Unmarshal([]byte(stdout), &statuses); err != nil {
		t.Fatalf("status all JSON invalid: %v\nraw: %s", err, stdout)
	}
	if len(statuses) == 0 {
		t.Fatal("status all JSON returned no statuses")
	}

	cacheFile := filepath.Join(root, ".campaign", "cache", "gitstatus", "status.json")
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Fatalf("status all wrote cache file %s, stat err = %v", cacheFile, err)
	}
}

func setupStatusAllTestCampaign(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}
	config := "id: test-status-all\nname: test-status-all\ntype: product\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}

	runStatusAllTestGit(t, root, "init", "-b", "main")
	runStatusAllTestGit(t, root, "config", "user.email", "test@example.com")
	runStatusAllTestGit(t, root, "config", "user.name", "Test User")

	if err := os.MkdirAll(filepath.Join(root, "projects", "alpha"), 0755); err != nil {
		t.Fatalf("mkdir submodule path: %v", err)
	}
	runStatusAllTestGit(t, root, "config", "-f", ".gitmodules", "submodule.alpha.path", "projects/alpha")
	runStatusAllTestGit(t, root, "config", "-f", ".gitmodules", "submodule.alpha.url", "https://example.com/alpha.git")
	runStatusAllTestGit(t, root, "add", ".campaign/campaign.yaml", ".gitmodules")
	runStatusAllTestGit(t, root, "commit", "-m", "initial")

	return root
}

func runStatusAllTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func captureStatusAllStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(out), runErr
}
