//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise Tab in zsh, including Cobra's filesystem fallback and no-file
// directive. A successful `camp __complete` response alone misses this bug.
func TestShellInit_ZshPluginCompletion(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)
	require.NoError(t, tc.WriteFile("/camp-bin/camp-app", `#!/bin/sh
case "$*" in
  '__complete open --quest '*) printf 'qst_review\tReview quest\n:4\n';;
  '__complete open README.md '*) printf ':4\n';;
  '__complete open '*) printf ':0\n';;
  *) printf ':4\n';;
esac
`))
	_, code, err := tc.ExecCommand("chmod", "+x", "/camp-bin/camp-app")
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NoError(t, tc.WriteFile("/test/README.md", "fixture"))
	_, code, err = tc.ExecCommand("touch", "/test/Project Notes.md")
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NoError(t, tc.WriteFile("/test/plugin-init.zsh", shellInitScript(t, tc, "zsh")))
	require.NoError(t, tc.WriteFile("/test/plugin-setup.zsh", `
PS1='>>> '
export PATH="/camp-bin:$PATH"
autoload -Uz compinit; compinit -u -d /tmp/plugin-compdump
unsetopt beep
source /test/plugin-init.zsh
cd /test
_dump_buffer() { print -r -- "$BUFFER" > /test/plugin-buffer.txt; zle send-break }
zle -N _dump_buffer
bindkey '^X^D' _dump_buffer
`))
	require.NoError(t, tc.WriteFile("/test/plugin-driver.zsh", `
zmodload zsh/zpty
rm -f /test/plugin-buffer.txt
zpty -b z sh -c 'stty rows 24 cols 100; TERM=xterm-256color exec zsh -f'
zpty -w z 'source /test/plugin-setup.zsh'
zpty -r z buf '*>>> '
zpty -w -n z "$1"; sleep 0.3
zpty -w -n z $'\t'; sleep 1
zpty -w -n z $'\x18\x04'
for i in 1 2 3 4 5 6 7 8; do sleep 0.5; [[ -f /test/plugin-buffer.txt ]] && break; done
zpty -d z
cat /test/plugin-buffer.txt
`))
	for _, test := range []struct{ input, want string }{
		{"camp app open REA", "camp app open README.md "},
		{"camp app open Pro", `camp app open Project\ Notes.md `},
		{"camp app open --quest qst_", "camp app open --quest qst_review "},
		{"camp app open README.md ", "camp app open README.md "},
	} {
		t.Run(test.input, func(t *testing.T) {
			out, code, err := tc.ExecCommand("zsh", "--no-rcs", "/test/plugin-driver.zsh", test.input)
			require.NoError(t, err)
			require.Equal(t, 0, code, "%s", out)
			require.Equal(t, test.want, strings.TrimRight(out, "\r\n"))
		})
	}
}
