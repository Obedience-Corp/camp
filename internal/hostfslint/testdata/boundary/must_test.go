package boundary

import "testing"

func TestDoesNotCallRunGit(t *testing.T) {
	// The identifier mustRunGit must not match the helper runGit.
	_ = "mustRunGit is a different name"
}
