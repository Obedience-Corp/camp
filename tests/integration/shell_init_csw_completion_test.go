//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestShellInit_ZshCswCompletionInsertsFuzzyMatch drives the real zsh line
// editor in the container: source the generated init with the real camp on
// PATH, type `csw <fuzzy prefix>`, press TAB, and read the editor buffer back
// through a bound widget. camp fuzzy-matches "cft" to the registered camp;
// the completer must hand that match to zsh in a form zsh inserts.
func TestShellInit_ZshCswCompletionInsertsFuzzyMatch(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)

	const camp = "Csw_Fuzzy_Target"
	_, err := tc.RunCamp("create", camp,
		"-d", "csw completion", "-m", "insert the fuzzy match", "--no-git", "--path", "/campaigns")
	require.NoError(t, err)

	initScript := shellInitScript(t, tc, "zsh")
	require.NoError(t, tc.WriteFile("/test/csw-init.zsh", initScript))

	require.NoError(t, tc.WriteFile("/test/csw-pty-setup.zsh", `
PS1='>>> '
export PATH="/camp-bin:$PATH"
autoload -Uz compinit; compinit -u -d /tmp/csw-compdump
unsetopt beep
source /test/csw-init.zsh
_dump_buffer() { print -r -- "$BUFFER" > /test/csw-buffer.txt; zle send-break }
zle -N _dump_buffer
bindkey '^X^D' _dump_buffer
`))

	driver := `
zmodload zsh/zpty
typed="$1"
rm -f /test/csw-buffer.txt
zpty -b z sh -c 'stty rows 24 cols 100; TERM=xterm-256color exec zsh -f'
zpty -w z 'source /test/csw-pty-setup.zsh'
zpty -r z buf '*>>> '
zpty -w -n z "csw $typed"; sleep 0.3
zpty -w -n z $'\t'; sleep 1
zpty -w -n z $'\x18\x04'
for i in 1 2 3 4 5 6 7 8; do sleep 0.5; [[ -f /test/csw-buffer.txt ]] && break; done
zpty -d z
cat /test/csw-buffer.txt 2>/dev/null || print -r -- "<no buffer dump>"
`
	require.NoError(t, tc.WriteFile("/test/csw-pty-driver.zsh", driver))

	run := func(typed string) string {
		t.Helper()
		output, exitCode, err := tc.ExecCommand("zsh", "--no-rcs", "/test/csw-pty-driver.zsh", typed)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "driver output: %s", output)
		return strings.TrimRight(output, "\r\n")
	}

	t.Run("fuzzy prefix inserts the registered camp", func(t *testing.T) {
		require.Equal(t, "csw "+camp+" ", run("cft"))
	})

	t.Run("case-folded prefix inserts the registered camp", func(t *testing.T) {
		require.Equal(t, "csw "+camp+" ", run("csw_fuzzy"))
	})

	t.Run("second argument is left alone", func(t *testing.T) {
		require.Equal(t, "csw "+camp+" ", run(camp+" "))
	})
}
