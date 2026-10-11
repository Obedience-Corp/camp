//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDungeonExploreShellEndOfOptions(t *testing.T) {
	exploreShellHop(t, "explore-shell-end", "camp dungeon explore --")
}

// A root flag is valid before or between the command words. The wrapper
// found `dungeon explore` only as the first two words, so these opened the
// feed without the handoff file and g left the shell where it was.
func TestDungeonExploreShellRootFlagBeforeCommand(t *testing.T) {
	for _, tc := range []struct{ name, campaign, line string }{
		{"flag first", "explore-shell-flag-first", "camp --no-color dungeon explore"},
		{"flag between the words", "explore-shell-flag-between", "camp dungeon --no-color=false explore"},
	} {
		t.Run(tc.name, func(t *testing.T) { exploreShellHop(t, tc.campaign, tc.line) })
	}
}

// exploreShellHop runs line in bash, zsh, and fish with shell integration
// loaded, presses g on the one finished item, and requires the shell to end
// up in that item's directory.
func exploreShellHop(t *testing.T, campaign, line string) {
	t.Helper()
	tc := GetSharedContainer(t)
	root := setupDungeonCampaign(t, tc, campaign)
	require.NoError(t, tc.WriteFile(root+"/dungeon/completed/finished.md", "# Finished fixture\n"))
	installShells(t, tc)
	tc.Shell(t, "mkdir -p /explore-bin && ln -sf /camp /explore-bin/camp")
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			require.NoError(t, tc.WriteFile("/tmp/explore-shell-init", shellInitScript(t, tc, sh)))
			script := "export PATH=/explore-bin:$PATH; source /tmp/explore-shell-init; " + line + "; pwd > /tmp/explore-shell-cwd"
			if sh == "fish" {
				script = "set -gx PATH /explore-bin $PATH; source /tmp/explore-shell-init; " + line + "; pwd > /tmp/explore-shell-cwd"
			}
			require.NoError(t, tc.WriteFile("/tmp/explore-shell-script", script))
			output, err := tc.runCampInteractive(root, nil, 30*time.Second, []InteractiveStep{{WaitFor: "finished.md", Input: "g"}}, "run", sh, "/tmp/explore-shell-script")
			require.NoError(t, err, "%s", output)
			selected, err := tc.ReadFile("/tmp/explore-shell-cwd")
			require.NoError(t, err)
			require.Equal(t, root+"/dungeon/completed", strings.TrimSpace(selected))
		})
	}
}

// The explorer writes the selected directory exactly, including a trailing or
// embedded newline. Command substitution would drop the trailing newline, and
// fish would also split on the embedded one, so g would enter a different path.
func TestDungeonExploreShellKeepsNewlinesInTheSelectedPath(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	require.NoError(t, tc.WriteFile("/tmp/explore-nl-setup.sh", exploreNewlineSetup))
	tc.Shell(t, "bash /tmp/explore-nl-setup.sh")
	root := setupDungeonCampaign(t, tc, "explore-nl-handoff")

	cases := []struct {
		name string
		file string
	}{
		{"trailing newline", "/tmp/explore-nl-path-trail"},
		{"embedded newline", "/tmp/explore-nl-path-embed"},
	}
	for _, sh := range []string{"bash", "zsh", "fish"} {
		init := shellInitScript(t, tc, sh)
		initPath := "/tmp/explore-nl-init-" + sh
		require.NoError(t, tc.WriteFile(initPath, init))
		for _, pathCase := range cases {
			t.Run(sh+"/"+pathCase.name, func(t *testing.T) {
				want, err := tc.ReadFile(pathCase.file)
				require.NoError(t, err)
				cwdFile := "/tmp/explore-nl-cwd"
				script := exploreNewlineScript(sh, initPath, pathCase.file, cwdFile)
				require.NoError(t, tc.WriteFile("/tmp/explore-nl-script", script))
				output, err := tc.runCampInteractive(root, nil, 30*time.Second, nil, "run", sh, "/tmp/explore-nl-script")
				require.NoError(t, err, "%s", output)
				recorded, err := tc.ReadFile(cwdFile)
				require.NoError(t, err)
				got := betweenMarkers(t, recorded, "PWD<", ">END")
				require.Equal(t, want, got, "shell %s output:\n%s", sh, output)
				require.Equal(t, "0", betweenMarkers(t, recorded, "STATUS<", ">"))
			})
		}
	}
}

const exploreNewlineSetup = `#!/bin/bash
set -eu
base=/tmp/explore-nl-dirs
rm -rf "$base"
mkdir -p "$base"
mkdir -p -- "$base"/$'task\n'
mkdir -p -- "$base"/$'em\nbed'
printf '%s' "$base"/$'task\n' > /tmp/explore-nl-path-trail
printf '%s' "$base"/$'em\nbed' > /tmp/explore-nl-path-embed
test -d "$base"/$'task\n'
test -d "$base"/$'em\nbed'
mkdir -p /tmp/explore-nl-bin
cat > /tmp/explore-nl-bin/camp << 'EOF'
#!/bin/sh
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--path-output" ]; then
    out="$arg"
  fi
  prev=$arg
done
if [ -z "$out" ] || [ -z "${CAMP_NL_PATHFILE:-}" ]; then
  echo "explore newline stub expected --path-output and CAMP_NL_PATHFILE" >&2
  exit 1
fi
cat "$CAMP_NL_PATHFILE" > "$out"
exit 0
EOF
chmod +x /tmp/explore-nl-bin/camp
`

func exploreNewlineScript(shell, initPath, pathFile, cwdFile string) string {
	// zsh's $status is read-only, so the exit code is stored under another name.
	if shell == "fish" {
		return "set -gx PATH /tmp/explore-nl-bin $PATH; set -gx CAMP_NL_PATHFILE " + pathFile +
			"; source " + initPath + "; camp dungeon explore; set -l cmd_status $status; printf 'PWD<%s>END\nSTATUS<%s>\n' \"$PWD\" \"$cmd_status\" > " + cwdFile
	}
	return "export PATH=/tmp/explore-nl-bin:$PATH; export CAMP_NL_PATHFILE=" + pathFile +
		"; source " + initPath + "; camp dungeon explore; cmd_status=$?; printf 'PWD<%s>END\nSTATUS<%s>\n' \"$PWD\" \"$cmd_status\" > " + cwdFile
}

func betweenMarkers(t *testing.T, text, open, endMark string) string {
	t.Helper()
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("missing %q in %q", open, text)
	}
	rest := text[start+len(open):]
	end := strings.Index(rest, endMark)
	if end < 0 {
		t.Fatalf("missing %q in %q", endMark, text)
	}
	return rest[:end]
}
