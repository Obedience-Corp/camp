//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupArtifactExplorer(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	root := setupGuardCampaign(t, tc, name)
	tc.Shell(t, fmt.Sprintf(`cd %s
 mkdir -p 'media/takes' 'exports [final]'
 printf script > media/script.md
 printf '/exports*/\n' > .gitignore
 printf 'version: 1\nroots:\n  - path: media\n  - path: media/takes\n  - path: exports [final]\n  - path: absent\n' > .campaign/artifacts.yaml
 git add .campaign media/script.md .gitignore
 git -c user.email=t@t -c user.name=t commit -qm fixture
 printf clip > media/clip.mp4
 printf take > 'media/takes/take two.mov'
 printf render > 'exports [final]/final.mp4'
 ln -s /missing media/link
 mkdir -p /artifact-bin
 ln -sf /camp /artifact-bin/camp
 cat > /artifact-bin/xdg-open <<'STUB'
#!/bin/sh
printf "%%s" "$1" > /tmp/artifact-opened
STUB
 chmod +x /artifact-bin/xdg-open
 `, root))
	return root
}

func TestArtifactsExplorerInventory(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-inventory")
	out, err := tc.RunCampInDir(root, "artifacts", "--json")
	require.NoError(t, err, "%s", out)
	var doc struct {
		Version int                       `json:"version"`
		Files   []artifacts.InventoryFile `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, "exports [final]/final.mp4", tc.GitOutput(t, root, "check-ignore", "exports [final]/final.mp4"))
	require.Equal(t, 1, doc.Version)
	require.Len(t, doc.Files, 4)
	assert.Equal(t, "exports [final]/final.mp4", doc.Files[0].Path, "ignored files and literal root names must be included")
	assert.Equal(t, "media/takes", doc.Files[3].Root, "overlapping roots must assign deepest owner without duplicates")
	assert.True(t, doc.Files[2].Symlink)
	plain, err := tc.RunCampInDir(root, "artifacts")
	require.NoError(t, err)
	assert.Contains(t, plain, "take two.mov")
	assert.NotContains(t, plain, "script.md")
	assert.NotContains(t, plain, "\x1b[")
	roots, err := tc.RunCampInDir(root, "artifacts", "list", "--json")
	require.NoError(t, err)
	assert.Contains(t, roots, `"roots"`)
	assert.NotContains(t, roots, `"files"`)
	tc.Shell(t, fmt.Sprintf("printf 'version: 1\nroots:\n  - path: ../outside\n' > %s/.campaign/artifacts.yaml", root))
	_, err = tc.RunCampInDir(root, "artifacts", "--json")
	require.Error(t, err)
}

func TestArtifactsExplorerTTY(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-explorer-tty")
	output, err := tc.runCampInteractive(root, map[string]string{"PATH": "/artifact-bin:/usr/local/bin:/usr/bin:/bin"}, 30*time.Second, []InteractiveStep{
		{WaitFor: "clip.mp4", Input: "/", WaitTimeout: 15 * time.Second},
		{WaitFor: "/ ", Input: "take two"},
		{WaitFor: "1–1 of 1", Input: "\r"},
		{Input: "o"},
		{WaitFor: "Opened take two.mov", Input: "g"},
	}, "artifacts", "--path-output", "/tmp/artifact-selected")
	require.NoError(t, err, "%s", output)
	selected, err := tc.ReadFile("/tmp/artifact-selected")
	require.NoError(t, err)
	assert.Equal(t, root+"/media/takes", selected)
	opened, err := tc.ReadFile("/tmp/artifact-opened")
	require.NoError(t, err)
	assert.Equal(t, root+"/media/takes/take two.mov", opened)
	// A failed opener remains visible and does not exit or navigate.
	tc.Shell(t, "printf '#!/bin/sh\necho unavailable >&2\nexit 1\n' > /artifact-bin/xdg-open")
	output, err = tc.RunCampInteractiveStepsInDirWithEnv(root, map[string]string{"PATH": "/artifact-bin:/usr/local/bin:/usr/bin:/bin"}, []InteractiveStep{
		{WaitFor: "clip.mp4", Input: "o", WaitTimeout: 15 * time.Second}, {WaitFor: "open final.mp4: exit status 1", Input: "q"},
	}, "artifacts")
	require.NoError(t, err, "%s", output)
}

func TestArtifactsExplorerShellNavigation(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-shell")
	installShells(t, tc)
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			init := shellInitScript(t, tc, sh)
			require.NoError(t, tc.WriteFile("/tmp/artifact-init", init))
			// The real shell wrapper and real explorer run in the same terminal. pwd
			// proves the parent shell changed directory, rather than only a child.
			script := "export PATH=/artifact-bin:$PATH; source /tmp/artifact-init; camp artifacts; pwd > /tmp/artifact-cwd"
			if sh == "fish" {
				script = "set -gx PATH /artifact-bin $PATH; source /tmp/artifact-init; camp artifacts; pwd > /tmp/artifact-cwd"
			}
			require.NoError(t, tc.WriteFile("/tmp/artifact-shell-script", script))
			output, err := tc.runCampInteractive(root, nil, 30*time.Second, []InteractiveStep{
				{WaitFor: "clip.mp4", Input: "/", WaitTimeout: 15 * time.Second}, {WaitFor: "/ ", Input: "take two"}, {WaitFor: "1–1 of 1", Input: "\r"}, {Input: "g"},
			}, "run", sh, "/tmp/artifact-shell-script")
			require.NoError(t, err, "%s", output)
			selected, err := tc.ReadFile("/tmp/artifact-cwd")
			require.NoError(t, err)
			assert.Equal(t, root+"/media/takes", strings.TrimSpace(selected))
		})
	}
}

func TestArtifactsExplorerOpenReleasesActions(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-open-release")
	// The opener outlives the action, like xdg-open waiting on a viewer.
	tc.Shell(t, "mkdir -p /artifact-open-bin && printf '#!/bin/sh\nsleep 20\n' > /artifact-open-bin/xdg-open && chmod +x /artifact-open-bin/xdg-open")
	output, err := tc.runCampInteractive(root, map[string]string{"PATH": "/artifact-open-bin:/usr/local/bin:/usr/bin:/bin"}, 30*time.Second, []InteractiveStep{
		{WaitFor: "clip.mp4", Input: "o", WaitTimeout: 15 * time.Second},
		{WaitFor: "Opened final.mp4", Input: "g"},
	}, "artifacts", "--path-output", "/tmp/artifact-open-release")
	require.NoError(t, err, "%s", output)
	selected, err := tc.ReadFile("/tmp/artifact-open-release")
	require.NoError(t, err)
	assert.Equal(t, root+"/exports [final]", selected)
}

func TestArtifactsExplorerClipboardPaste(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-paste")
	tc.Shell(t, "mkdir -p /artifact-paste-bin && printf '#!/bin/sh\nprintf \"take two\"\n' > /artifact-paste-bin/xclip && chmod +x /artifact-paste-bin/xclip")
	output, err := tc.runCampInteractive(root, map[string]string{"PATH": "/artifact-paste-bin:/usr/local/bin:/usr/bin:/bin"}, 30*time.Second, []InteractiveStep{
		{WaitFor: "clip.mp4", Input: "/", WaitTimeout: 15 * time.Second},
		{WaitFor: "/ ", Input: "\x16"},
		{WaitFor: "1–1 of 1", Input: "\r"},
		{Input: "g"},
	}, "artifacts", "--path-output", "/tmp/artifact-paste")
	require.NoError(t, err, "%s", output)
	selected, err := tc.ReadFile("/tmp/artifact-paste")
	require.NoError(t, err)
	assert.Equal(t, root+"/media/takes", selected)
}

func TestArtifactsExplorerShellLeadingFlags(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupArtifactExplorer(t, tc, "artifact-shell-flags")
	installShells(t, tc)
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			init := shellInitScript(t, tc, sh)
			require.NoError(t, tc.WriteFile("/tmp/artifact-flags-init", init))
			// stdout stays on the terminal: the wrapper passes everything through
			// when it is redirected, which would hide the subcommand check.
			script := "export PATH=/artifact-bin:$PATH; source /tmp/artifact-flags-init; camp artifacts --no-color list; echo \"list-status=$?\"; camp artifacts --no-color --; pwd > /tmp/artifact-flags-cwd"
			if sh == "fish" {
				script = "set -gx PATH /artifact-bin $PATH; source /tmp/artifact-flags-init; camp artifacts --no-color list; echo \"list-status=$status\"; camp artifacts --no-color --; pwd > /tmp/artifact-flags-cwd"
			}
			require.NoError(t, tc.WriteFile("/tmp/artifact-flags-script", script))
			output, err := tc.runCampInteractive(root, nil, 30*time.Second, []InteractiveStep{
				{WaitFor: "list-status=", WaitTimeout: 15 * time.Second},
				{WaitFor: "clip.mp4", Input: "/", WaitTimeout: 15 * time.Second}, {WaitFor: "/ ", Input: "take two"}, {WaitFor: "1–1 of 1", Input: "\r"}, {Input: "g"},
			}, "run", sh, "/tmp/artifact-flags-script")
			require.NoError(t, err, "%s", output)
			assert.Contains(t, output, "list-status=0")
			assert.Contains(t, output, "media/takes")
			assert.NotContains(t, output, "Usage:")
			selected, err := tc.ReadFile("/tmp/artifact-flags-cwd")
			require.NoError(t, err)
			assert.Equal(t, root+"/media/takes", strings.TrimSpace(selected))
		})
	}
}
