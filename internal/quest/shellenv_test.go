package quest

import (
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseShellDialect(t *testing.T) {
	posix := []string{"", "posix", "sh", "bash", "zsh", "  BASH "}
	for _, in := range posix {
		got, err := ParseShellDialect(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, ShellPosix, got, "input %q", in)
	}

	got, err := ParseShellDialect("fish")
	require.NoError(t, err)
	assert.Equal(t, ShellFish, got)

	_, err = ParseShellDialect("powershell")
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
}

func TestRenderActivate(t *testing.T) {
	assert.Equal(t, "export CAMP_QUEST='qst_20260710_ab12cd'",
		RenderActivate(ShellPosix, "qst_20260710_ab12cd"))
	assert.Equal(t, "set -gx CAMP_QUEST 'qst_20260710_ab12cd'",
		RenderActivate(ShellFish, "qst_20260710_ab12cd"))
}

func TestRenderClear(t *testing.T) {
	assert.Equal(t, "unset CAMP_QUEST", RenderClear(ShellPosix))
	assert.Equal(t, "set -e CAMP_QUEST", RenderClear(ShellFish))
}

func TestRenderActivate_QuotingIsInjectionSafe(t *testing.T) {
	// A hostile value must remain a single quoted literal, never break out.
	posix := RenderActivate(ShellPosix, "x'; rm -rf /; '")
	assert.Equal(t, `export CAMP_QUEST='x'\''; rm -rf /; '\'''`, posix)

	fish := RenderActivate(ShellFish, `x\'y`)
	assert.Equal(t, `set -gx CAMP_QUEST 'x\\\'y'`, fish)
}
