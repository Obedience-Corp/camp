//go:build integration
// +build integration

package integration

import (
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
}
