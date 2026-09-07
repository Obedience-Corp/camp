//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSwitch_BottomProximityTTY drives compiled `camp switch` against a
// three-camp registry in the container and checks the most recent camp
// (gamma) is listed next to the prompt.
func TestSwitch_BottomProximityTTY(t *testing.T) {
	tc := GetSharedContainer(t)

	home := "/tmp/switch-selector-home"
	registry := home + "/registry.json"
	tc.Shell(t, `
set -e
mkdir -p `+home+`/camps/alpha `+home+`/camps/beta `+home+`/camps/gamma
cat > `+registry+` <<'EOF'
{
  "version": 2,
  "campaigns": {
    "id-alpha": {
      "name": "alpha",
      "path": "/tmp/switch-selector-home/camps/alpha",
      "type": "product",
      "status": "active",
      "last_access": "2026-01-01T00:00:00Z"
    },
    "id-beta": {
      "name": "beta",
      "path": "/tmp/switch-selector-home/camps/beta",
      "type": "product",
      "status": "active",
      "last_access": "2026-06-01T00:00:00Z"
    },
    "id-gamma": {
      "name": "gamma",
      "path": "/tmp/switch-selector-home/camps/gamma",
      "type": "product",
      "status": "active",
      "last_access": "2026-09-01T00:00:00Z"
    }
  }
}
EOF
`)

	output, err := tc.runCampInteractive(
		home+"/camps/alpha",
		map[string]string{"CAMP_REGISTRY_PATH": registry},
		20*time.Second,
		[]InteractiveStep{
			{WaitFor: "Switch to:", Input: "\x03"},
		},
		"switch",
	)
	require.Contains(t, output, "Switch to:", "switch TTY session failed (err=%v):\n%s", err, output)
	assert.Contains(t, output, "alpha")
	assert.Contains(t, output, "beta")
	assert.Contains(t, output, "gamma")

	// Ctrl+C restores the main screen. Read CUP row addresses from the last
	// picker paint: go-fuzzyfinder puts index 0 (gamma) on the prompt row.
	frozen := ttyFreezeAtPrompt(output, "Switch to:", "gamma")
	rows := ttyCUPRows(frozen)
	promptRow, gammaRow, alphaRow := 0, 0, 0
	for row, text := range rows {
		if strings.Contains(text, "Switch to:") && row > promptRow {
			promptRow = row
		}
		if strings.Contains(text, "gamma") && row > gammaRow {
			gammaRow = row
		}
		if strings.Contains(text, "alpha") && row > alphaRow {
			alphaRow = row
		}
	}
	require.NotZero(t, promptRow, "CUP rows missing Switch to: %v", rows)
	require.NotZero(t, gammaRow, "CUP rows missing gamma: %v", rows)
	assert.Greater(t, gammaRow, alphaRow,
		"gamma should be below alpha (bottom-proximity); alpha row %d gamma row %d prompt row %d rows=%v",
		alphaRow, gammaRow, promptRow, rows)
	assert.LessOrEqual(t, promptRow-gammaRow, 3,
		"gamma should be adjacent to Switch to: (gamma row %d prompt row %d rows=%v)",
		gammaRow, promptRow, rows)
}

func TestTTYCUPRows_PromptAdjacency(t *testing.T) {
	raw := "\x1b[36;1H\x1b(B\x1b[m  default/alpha\x1b[37;1H\x1b(B\x1b[m  default/beta" +
		"\x1b[38;1H\x1b[7m>\x1b[27m default/gamma\x1b[39;1H\x1b(B\x1b[m\x1b[32m  ↑/↓ navigate" +
		"\x1b[40;1H\x1b(B\x1b[m\x1b[34mSwitch to: "
	rows := ttyCUPRows(raw)
	assert.Contains(t, rows[40], "Switch to:")
	assert.Contains(t, rows[38], "gamma")
	assert.Contains(t, rows[36], "alpha")
	assert.Greater(t, 38, 36)
	assert.LessOrEqual(t, 40-38, 3)
}

func TestTTYVisibleLines_PromptAdjacency(t *testing.T) {
	raw := "\x1b[?1049h\x1b[2J\x1b[H" +
		"default/alpha\r\n" +
		"default/beta\r\n" +
		"> default/gamma\r\n" +
		"3/3\r\n" +
		"Switch to: \r\n" +
		"\x1b[?1049lcleared"
	lines := ttyVisibleLines(raw, 10, 40)
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "gamma")
	assert.Contains(t, joined, "Switch to:")
	prompt := 0
	for i, line := range lines {
		if strings.Contains(line, "Switch to:") {
			prompt = i
		}
	}
	near := strings.Join(lines[prompt-2:prompt+1], "\n")
	assert.Contains(t, near, "gamma")
	assert.NotContains(t, near, "alpha")
}
