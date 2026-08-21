package helpercaller

import "testing"

func TestCloneViaHelper(t *testing.T) {
	runGit(t, t.TempDir(), "status")
}
