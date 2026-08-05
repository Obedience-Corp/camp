package clone

import (
	"strings"
	"testing"
)

func TestPeerRepoPath(t *testing.T) {
	const root = "/home/peer/campaigns/demo"
	tests := []struct {
		name string
		repo string
		want string
	}{
		{name: "campaign root marker", repo: ".", want: root},
		{name: "empty repo is the root", repo: "", want: root},
		{name: "submodule", repo: "projects/camp", want: root + "/projects/camp"},
		{name: "nested submodule", repo: "a/b/c", want: root + "/a/b/c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := peerRepoPath(root, tt.repo); got != tt.want {
				t.Errorf("peerRepoPath(%q, %q) = %q, want %q", root, tt.repo, got, tt.want)
			}
		})
	}
}

// TestBundleRefspecsAreForced guards the seed contract: a bundle is a snapshot
// of the peer, so its refs must land verbatim rather than being subject to
// fast-forward rules against whatever the fresh destination happens to have.
func TestBundleRefspecsAreForced(t *testing.T) {
	if len(bundleRefspecs) == 0 {
		t.Fatal("bundleRefspecs must not be empty")
	}
	for _, spec := range bundleRefspecs {
		if !strings.HasPrefix(spec, "+") {
			t.Errorf("refspec %q must be forced (leading +)", spec)
		}
		if !strings.Contains(spec, ":") {
			t.Errorf("refspec %q must map a source to a destination", spec)
		}
	}
}
