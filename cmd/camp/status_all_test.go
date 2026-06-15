package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
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
	installStatusAllFakeGit(t)
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
	if err := os.MkdirAll(filepath.Join(root, "projects", "alpha"), 0755); err != nil {
		t.Fatalf("mkdir submodule path: %v", err)
	}
	return root
}

func installStatusAllFakeGit(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir fake git dir: %v", err)
	}
	script := `#!/bin/sh
repo=""
if [ "$1" = "-C" ]; then
	repo="$2"
	shift 2
fi

if [ "$1" = "config" ] && [ "$2" = "-f" ] && [ "$3" = ".gitmodules" ] && [ "$4" = "--list" ]; then
	case "$repo" in
		*/projects/alpha)
			exit 1
			;;
	esac
	printf '%s\n' \
		"submodule.alpha.path=projects/alpha" \
		"submodule.alpha.url=https://example.com/alpha.git"
	exit 0
fi

if [ "$1" = "rev-parse" ] && [ "$2" = "--abbrev-ref" ] && [ "$3" = "HEAD" ]; then
	printf 'main\n'
	exit 0
fi

if [ "$1" = "remote" ]; then
	printf 'origin\n'
	exit 0
fi

if [ "$1" = "status" ]; then
	exit 0
fi

if [ "$1" = "submodule" ] && [ "$2" = "status" ]; then
	exit 0
fi

if [ "$1" = "rev-list" ]; then
	printf '0	0\n'
	exit 0
fi

if [ "$1" = "symbolic-ref" ]; then
	printf 'origin/main\n'
	exit 0
fi

if [ "$1" = "branch" ]; then
	exit 0
fi

if [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ]; then
	exit 0
fi

printf 'unexpected fake git invocation: repo=%s args=%s\n' "$repo" "$*" >&2
exit 1
`
	gitPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
