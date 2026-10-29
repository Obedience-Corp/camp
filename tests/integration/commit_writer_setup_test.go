//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writerSetupCamp(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	root := "/campaigns/" + name
	_, err := tc.InitCampaign(root, name, "product")
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile(root+"/change.txt", "a change\n"))
	return root
}

func TestCommitWriterSetup_CustomResumesCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	root := writerSetupCamp(t, tc, "writer-setup-custom")
	tc.Shell(t, "printf '\n# Keep this comment\ncustom_metadata: preserved\nhooks:\n  commit_message:\n    timeout: 3m\n' >> "+root+"/.campaign/campaign.yaml")
	output, err := tc.RunCampInteractiveStepsInDir(root, []InteractiveStep{
		{WaitFor: "Set up automatic commit messages", Input: "\x1b[B\r"},
		{WaitFor: "Commit message command", Input: "printf 'custom writer message'\r"},
	}, "commit", "--auto-write")
	require.NoError(t, err, output)
	assert.Contains(t, headSubject(t, tc, root), "custom writer message")
	data, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, data, "command: printf 'custom writer message'")
	assert.Contains(t, data, "timeout: 3m")
	assert.Contains(t, data, "# Keep this comment")
	assert.Contains(t, data, "custom_metadata: preserved")
	// Configured invocations go straight to the writer, including without a TTY.
	require.NoError(t, tc.WriteFile(root+"/change.txt", "second change\n"))
	output, err = tc.RunCampInDir(root, "commit", "--auto-write")
	require.NoError(t, err, output)
	assert.NotContains(t, output, "Set up automatic")
}

func TestCommitWriterSetup_ManualAndCancel(t *testing.T) {
	tc := GetSharedContainer(t)
	for _, manual := range []bool{false, true} {
		name := "writer-setup-cancel"
		if manual {
			name = "writer-setup-manual"
		}
		t.Run(name, func(t *testing.T) {
			root := writerSetupCamp(t, tc, name)
			before, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
			require.NoError(t, err)
			head := headSubject(t, tc, root)
			input := "\x03"
			if manual {
				input = "\x1b[B\x1b[B\r"
			}
			steps := []InteractiveStep{{WaitFor: "Set up automatic commit messages", Input: input}}
			if manual {
				steps = append(steps, InteractiveStep{WaitFor: "Used for this commit only", Input: "manual message\r"})
			}
			output, err := tc.RunCampInteractiveStepsInDir(root, steps, "commit", "--auto-write")
			if manual {
				require.NoError(t, err, output)
				assert.Contains(t, headSubject(t, tc, root), "manual message")
			} else {
				require.Error(t, err)
				assert.Equal(t, head, headSubject(t, tc, root))
				assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, root, "diff", "--cached", "--name-only")))
			}
			after, readErr := tc.ReadFile(root + "/.campaign/campaign.yaml")
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
		})
	}
}

func TestCommitWriterSetup_Noninteractive(t *testing.T) {
	tc := GetSharedContainer(t)
	for _, flags := range [][]string{{}, {"--json"}, {"--background", "--json"}} {
		name := "writer-no-tty" + strings.ReplaceAll(strings.Join(flags, ""), "-", "")
		root := writerSetupCamp(t, tc, name)
		args := append([]string{"commit", "--auto-write"}, flags...)
		output, err := tc.RunCampInDir(root, args...)
		require.Error(t, err)
		assert.Contains(t, output, "camp settings commit-writer")
		assert.NotContains(t, output, "Use a custom command")
		assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, root, "diff", "--cached", "--name-only")))
	}
}

func TestCommitWriterSetup_SettingsEditAndInstallerFallback(t *testing.T) {
	tc := GetSharedContainer(t)
	root := writerSetupCamp(t, tc, "writer-settings")
	output, err := tc.RunCampInteractiveStepsInDir(root, []InteractiveStep{
		{WaitFor: "Install Obey with Festival", Input: "\r"},
		{WaitFor: "Festival isn't available", Input: "\x1b[B\r"},
		{WaitFor: "Commit message command", Input: "printf first\r"},
	}, "settings", "commit-writer")
	require.NoError(t, err, output)
	output, err = tc.RunCampInteractiveStepsInDir(root, []InteractiveStep{
		{WaitFor: "Current command: printf first", Input: "\x1b[B\r"},
		{WaitFor: "Commit message command", Input: "\x01\x0bprintf second\r"},
	}, "settings", "commit-writer")
	require.NoError(t, err, output)
	data, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, data, "command: printf second")
}

func TestCommitWriterSetup_InstallObey(t *testing.T) {
	tc := GetSharedContainer(t)
	defer tc.Shell(t, "rm -f /usr/local/bin/ob /usr/local/bin/festival")
	root := writerSetupCamp(t, tc, "writer-install")
	const bin = "/usr/local/bin"
	require.NoError(t, tc.WriteFile(bin+"/festival", `#!/bin/sh
[ "$*" = 'install obey' ] || exit 9
cat > /usr/local/bin/ob <<'SCRIPT'
#!/bin/sh
[ "$*" = 'commit --print-session-id' ] || exit 10
printf 'obey fixture message'
SCRIPT
chmod +x /usr/local/bin/ob
`))
	tc.Shell(t, "chmod +x "+bin+"/festival")
	output, err := tc.RunCampInteractiveStepsInDirWithEnv(root, map[string]string{"PATH": bin + ":/usr/local/bin:/usr/bin:/bin"}, []InteractiveStep{
		{WaitFor: "Install Obey with Festival", Input: "\r"},
	}, "commit", "--auto-write")
	require.NoError(t, err, output)
	assert.Contains(t, headSubject(t, tc, root), "obey fixture message")
	data, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, data, "command: ob commit --print-session-id")
}

func TestCommitWriterSetup_ProjectAndBrokenWriter(t *testing.T) {
	tc := GetSharedContainer(t)
	root := writerSetupCamp(t, tc, "writer-project")
	project := root + "/projects/app"
	tc.Shell(t, "mkdir -p "+project+" && git -C "+project+" init -q && git -C "+project+" config user.name Fixture && git -C "+project+" config user.email fixture@example.invalid")
	require.NoError(t, tc.WriteFile(project+"/app.txt", "project change\n"))
	output, err := tc.RunCampInteractiveStepsInDir(project, []InteractiveStep{
		{WaitFor: "Set up automatic commit messages", Input: "\x1b[B\r"},
		{WaitFor: "Commit message command", Input: "printf 'project writer message'\r"},
	}, "p", "commit", "--auto-write", "--no-sync")
	require.NoError(t, err, output)
	assert.Contains(t, headSubject(t, tc, project), "project writer message")
	data, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, data, "project writer message")
	// A configured runtime failure must never reopen first-use setup.
	data = strings.ReplaceAll(data, "printf 'project writer message'", "exit 42")
	require.NoError(t, tc.WriteFile(root+"/.campaign/campaign.yaml", data))
	output, err = tc.RunCampInteractiveStepsInDir(root, nil, "commit", "--auto-write")
	require.Error(t, err)
	assert.Contains(t, output, "commit message command failed")
	assert.NotContains(t, output, "Set up automatic")
}

func TestCommitWriterSetup_ObeyAvailableAndInstallFailure(t *testing.T) {
	tc := GetSharedContainer(t)
	defer tc.Shell(t, "rm -f /usr/local/bin/ob /usr/local/bin/festival")
	root := writerSetupCamp(t, tc, "writer-available")
	require.NoError(t, tc.WriteFile("/usr/local/bin/ob", "#!/bin/sh\nprintf 'existing obey message'\n"))
	tc.Shell(t, "chmod +x /usr/local/bin/ob")
	output, err := tc.RunCampInteractiveStepsInDir(root, []InteractiveStep{
		{WaitFor: "Use Obey", Input: "\r"},
	}, "commit", "--auto-write")
	require.NoError(t, err, output)
	assert.NotContains(t, output, "Install Obey with Festival")
	assert.Contains(t, headSubject(t, tc, root), "existing obey message")

	tc.Shell(t, "rm /usr/local/bin/ob")
	root = writerSetupCamp(t, tc, "writer-install-failure")
	before, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	require.NoError(t, tc.WriteFile("/usr/local/bin/festival", "#!/bin/sh\necho 'fixture install failure' >&2\nexit 1\n"))
	tc.Shell(t, "chmod +x /usr/local/bin/festival")
	output, err = tc.RunCampInteractiveStepsInDir(root, []InteractiveStep{
		{WaitFor: "Install Obey with Festival", Input: "\r"},
		{WaitFor: "Obey installation failed", Input: "\x03"},
	}, "commit", "--auto-write")
	require.Error(t, err)
	assert.Contains(t, output, "fixture install failure")
	after, readErr := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	assert.Empty(t, strings.TrimSpace(tc.GitOutput(t, root, "diff", "--cached", "--name-only")))
}

// Festival can change a login shell's PATH without changing its parent Camp
// process. Detect and execute Obey in that same shell after installation.
func TestCommitWriterSetup_InstallUpdatesShellPath(t *testing.T) {
	tc := GetSharedContainer(t)
	defer tc.Shell(t, "rm -f /usr/local/bin/ob /usr/local/bin/festival")
	root := writerSetupCamp(t, tc, "writer-shell-path")
	const bin = "/writer-new-path"
	require.NoError(t, tc.WriteFile("/usr/local/bin/festival", `#!/bin/sh
mkdir -p /writer-new-path
cat > /writer-new-path/ob <<'SCRIPT'
#!/bin/sh
printf 'installed through shell path'
SCRIPT
chmod +x /writer-new-path/ob
`))
	require.NoError(t, tc.WriteFile("/writer-shell", `#!/bin/sh
export PATH=/writer-new-path:$PATH
exec /bin/sh -c "$2"
`))
	tc.Shell(t, "chmod +x /usr/local/bin/festival /writer-shell")
	output, err := tc.RunCampInteractiveStepsInDirWithEnv(root, map[string]string{"SHELL": "/writer-shell"}, []InteractiveStep{
		{WaitFor: "Install Obey with Festival", Input: "\r"},
	}, "commit", "--auto-write")
	require.NoError(t, err, output)
	assert.Contains(t, headSubject(t, tc, root), "installed through shell path")
	// It was not necessary to persist a machine-specific executable path.
	data, err := tc.ReadFile(root + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, data, "command: ob commit --print-session-id")
	assert.NotContains(t, data, bin)
}
