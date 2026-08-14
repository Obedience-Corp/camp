//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRenameCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	root := "/campaigns/" + name
	_, err := tc.InitCampaign(root, name, "product")
	require.NoError(t, err)
	return root
}

func TestIntegration_ProjectRenameSubmodulePreservesWorktreesAndMigratesMetadata(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-submodule")

	tc.Shell(t, fmt.Sprintf(`
		set -eu
		git init -q --bare /remotes/project-rename-old.git
		git init -q --bare /remotes/project-rename-new.git
		git init -q /tmp/project-rename-source
		cd /tmp/project-rename-source
		printf 'initial\n' > README.md
		git add README.md
		git commit -q -m initial
		git remote add origin /remotes/project-rename-old.git
		git push -q origin HEAD

		cd %[1]s
		git -c protocol.file.allow=always submodule add -q /remotes/project-rename-old.git projects/widget
		git add .gitmodules projects/widget
		git commit -q -m 'add widget'
		mkdir -p projects/worktrees/widget
		git -C projects/widget worktree add -q -b feature %[1]s/projects/worktrees/widget/feature
		printf 'dirty main\n' > projects/widget/main-dirty.txt
		printf 'dirty worktree\n' > projects/worktrees/widget/feature/worktree-dirty.txt

		mkdir -p workflow/design/widget .campaign/settings .campaign/workitems .campaign/leverage/snapshots/widget
		cat > workflow/design/widget/.workitem <<'EOF'
version: v1alpha8
kind: workitem
id: design-widget-2026-08-13
type: design
title: Widget
ref: WI-123abc
projects:
  - projects/widget
EOF
		cat > .campaign/settings/fresh.yaml <<'EOF'
projects:
  widget:
    branch: feature
EOF
		cat > .campaign/settings/pins.json <<'EOF'
[{"name":"widget","path":"projects/widget/docs","created_at":"2026-08-13T00:00:00Z"}]
EOF
		cat > .campaign/workitems/links.yaml <<'EOF'
version: workitem-links/v1alpha1
links:
  - id: lnk_20260813_123abc
    workitem_id: design-widget-2026-08-13
    scope:
      kind: worktree
      path: projects/worktrees/widget/feature
    role: primary
    created_at: 2026-08-13T00:00:00Z
    created_by: test
EOF
		cat > .campaign/leverage/config.json <<'EOF'
{"projects":{"widget":{"path":"projects/widget","include":true}}}
EOF
		cat > .campaign/leverage/snapshots/widget/2026-08-13.json <<'EOF'
{"project":"widget","date":"2026-08-13"}
EOF
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "widget", "renamed",
		"--remote-url", "/remotes/project-rename-new.git", "--no-verify", "--no-commit")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "Project renamed")

	state := tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		test ! -e projects/widget
		test -d projects/renamed
		test -f projects/renamed/main-dirty.txt
		test -f projects/worktrees/renamed/feature/worktree-dirty.txt
		git -C projects/renamed status --porcelain
		git -C projects/worktrees/renamed/feature status --porcelain
		git -C projects/renamed worktree list --porcelain
		git config -f .gitmodules --get submodule.projects/renamed.path
		git config -f .gitmodules --get submodule.projects/renamed.url
		git -C projects/renamed remote get-url origin
		grep -R 'projects/renamed' workflow/design/widget/.workitem .campaign/settings/pins.json .campaign/workitems/links.yaml .campaign/leverage/config.json
		grep -q 'renamed:' .campaign/settings/fresh.yaml
		grep -q '"project": "renamed"' .campaign/leverage/snapshots/renamed/2026-08-13.json
	`, root))
	assert.Contains(t, state, "main-dirty.txt")
	assert.Contains(t, state, "worktree-dirty.txt")
	assert.Contains(t, state, "projects/worktrees/renamed/feature")
	assert.Contains(t, state, "/remotes/project-rename-new.git")
}

func TestIntegration_ProjectRenameCampaignOwnedDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-campaign-dir")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		mkdir -p projects/handbook
		printf 'tracked\n' > projects/handbook/README.md
		git add projects/handbook/README.md
		git commit -q -m 'add handbook'
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "handbook", "guides")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "campaign-directory")
	assert.Contains(t, tc.GitOutput(t, root, "log", "-1", "--format=%s"), "Rename: handbook -> guides")
	assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, root, "status", "--short", "--", "projects/handbook", "projects/guides")))
}

func TestIntegration_ProjectRenameSkipsAutomaticCommitForPreexistingChanges(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-preexisting-change")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		mkdir -p projects/notes
		printf 'tracked\n' > projects/notes/README.md
		git add projects/notes/README.md
		git commit -q -m 'add notes'
		printf 'user change\n' >> projects/notes/README.md
	`, root))
	before := strings.TrimSpace(tc.GitOutput(t, root, "rev-parse", "HEAD"))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "notes", "knowledge")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "automatic commit would mix them with the rename")
	assert.Equal(t, before, strings.TrimSpace(tc.GitOutput(t, root, "rev-parse", "HEAD")))
	assert.Contains(t, tc.GitOutput(t, root, "status", "--short"), "projects/knowledge/README.md")
	assert.Contains(t, tc.Shell(t, fmt.Sprintf("cat %s/projects/knowledge/README.md", root)), "user change")
}

func TestIntegration_ProjectRenameLinkedWorkspaceKeepsExternalTarget(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-linked")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		git init -q /tmp/project-rename-linked-source
		cd /tmp/project-rename-linked-source
		printf 'linked\n' > README.md
		git add README.md
		git commit -q -m initial
		cd %[1]s
		ln -s /tmp/project-rename-linked-source projects/local-old
		git add projects/local-old
		git commit -q -m 'link local project'
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "mv", "local-old", "local-new", "--no-commit")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "linked")
	target := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		test ! -e projects/local-old
		readlink projects/local-new
		test -f /tmp/project-rename-linked-source/README.md
	`, root)))
	assert.Equal(t, "/tmp/project-rename-linked-source", target)
}

func TestIntegration_ProjectRenameDryRunIsReadOnlyJSON(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-dry-run")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		mkdir -p projects/docs-old
		printf 'tracked\n' > projects/docs-old/README.md
		git add projects/docs-old/README.md
		git commit -q -m 'add docs'
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "docs-old", "docs-new", "--dry-run", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, `"schema_version": "project-rename/v1alpha1"`)
	assert.Contains(t, stdout, `"dry_run": true`)
	exists, err := tc.CheckDirExists(root + "/projects/docs-old")
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = tc.CheckDirExists(root + "/projects/docs-new")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestIntegration_ProjectRenameUninitializedSubmodule(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-uninitialized")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		git init -q --bare /remotes/project-rename-uninitialized.git
		git init -q /tmp/project-rename-uninitialized-source
		cd /tmp/project-rename-uninitialized-source
		printf 'initial\n' > README.md
		git add README.md
		git commit -q -m initial
		git remote add origin /remotes/project-rename-uninitialized.git
		git push -q origin HEAD
		cd %[1]s
		git -c protocol.file.allow=always submodule add -q /remotes/project-rename-uninitialized.git projects/cold
		git add .gitmodules projects/cold
		git commit -q -m 'add cold'
		git submodule deinit -q -f -- projects/cold
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "cold", "warm", "--no-commit")
	require.NoError(t, err)
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "submodule")
	assert.Equal(t, "projects/warm", strings.TrimSpace(tc.GitOutput(t, root,
		"config", "-f", ".gitmodules", "--get", "submodule.projects/warm.path")))
	localConfig := tc.Shell(t, fmt.Sprintf(`cd %s && git config --get-regexp '^submodule\.projects/warm\.' || true`, root))
	assert.Empty(t, strings.TrimSpace(localConfig))
}

func TestIntegration_ProjectRenameRollsBackAfterRemoteMutationFailure(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupRenameCampaign(t, tc, "project-rename-rollback")
	tc.Shell(t, fmt.Sprintf(`
		set -eu
		git init -q /tmp/project-rename-rollback-linked
		cd /tmp/project-rename-rollback-linked
		printf 'initial\n' > README.md
		git add README.md
		git commit -q -m initial
		cd %[1]s
		ln -s /tmp/project-rename-rollback-linked projects/original
		git add projects/original
		git commit -q -m 'link original'
	`, root))

	stdout, stderr, code, err := tc.RunCampSplitInDir(root,
		"project", "rename", "original", "attempted",
		"--remote-url", "/remotes/project-rename-rollback.git", "--no-verify", "--no-commit")
	require.NoError(t, err)
	assert.Equal(t, 1, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stderr, "set project origin URL")

	state := tc.Shell(t, fmt.Sprintf(`
		set -eu
		cd %[1]s
		test -L projects/original
		test ! -e projects/attempted
		git -C projects/original status --porcelain
		git status --porcelain
		test -z "$(find .git/camp/transactions -type f -name 'project-rename-*.json' -print -quit)"
	`, root))
	assert.Empty(t, strings.TrimSpace(state))
	assert.NotContains(t, state, "projects/attempted")
}
