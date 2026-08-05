package clone

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCloneJSONOmitsSeedWithoutPeer is the golden guard for the festival's
// byte-identical-defaults requirement: a clone that used no peer must emit
// exactly the JSON it emitted before peer seeding existed. Asserting on the raw
// bytes rather than the decoded struct is the point — an accidental `"seed":
// null` or `"seed": {}` would decode the same and break every existing
// consumer.
func TestCloneJSONOmitsSeedWithoutPeer(t *testing.T) {
	tests := []struct {
		name   string
		result *CloneResult
	}{
		{
			name:   "successful clone",
			result: &CloneResult{Success: true, Directory: "/campaigns/demo", Branch: "main"},
		},
		{
			name: "clone with submodules and warnings",
			result: &CloneResult{
				Success: true, Directory: "/campaigns/demo", Branch: "main",
				Submodules: []SubmoduleResult{{Name: "camp", Path: "projects/camp", Success: true}},
				Warnings:   []string{"something worth noting"},
			},
		},
		{
			name:   "failed clone",
			result: &CloneResult{Success: false, Directory: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := tt.result.JSON()
			if err != nil {
				t.Fatalf("JSON() error = %v", err)
			}
			if strings.Contains(string(raw), "seed") {
				t.Errorf("clone without a peer emitted a seed key; output must be unchanged:\n%s", raw)
			}
		})
	}
}

func TestCloneJSONReportsSeedPerRepo(t *testing.T) {
	result := &CloneResult{
		Success: true, Directory: "/campaigns/demo", Branch: "main",
		Seed: []SeedRepoResult{
			{Repo: ".", Method: SeedMethodPackCopy},
			{Repo: "projects/camp", Method: SeedMethodBundle, Reason: "peer repository is not quiescent"},
			{Repo: "projects/fest", Method: SeedMethodOrigin, Reason: "peer unreachable"},
		},
	}

	raw, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var decoded JSONOutput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Seed == nil {
		t.Fatalf("seed summary missing from output:\n%s", raw)
	}
	if len(decoded.Seed.Repos) != 3 {
		t.Fatalf("seed reported %d repos, want 3", len(decoded.Seed.Repos))
	}

	// The root is reported first and by its canonical marker, so a consumer can
	// find it without knowing the campaign's directory name.
	if got := decoded.Seed.Repos[0]; got.Repo != "." || got.Method != SeedMethodPackCopy {
		t.Errorf("first seed entry = %+v, want the campaign root via pack-copy", got)
	}
	// A fallback carries the reason; the fast path does not invent one.
	if decoded.Seed.Repos[0].Reason != "" {
		t.Errorf("pack-copied repo should carry no fallback reason, got %q", decoded.Seed.Repos[0].Reason)
	}
	if !strings.Contains(decoded.Seed.Repos[1].Reason, "not quiescent") {
		t.Errorf("bundle entry should explain the fallback, got %q", decoded.Seed.Repos[1].Reason)
	}
}

// TestCloneJSONReportsAnUnreachablePeer covers the case that otherwise
// disappears: --from named a machine camp could not reach, the human warning is
// suppressed in JSON mode, and no peer was configured — so without an explicit
// record the output is indistinguishable from a plain clone.
func TestCloneJSONReportsAnUnreachablePeer(t *testing.T) {
	c := NewCloner(WithPeerUnavailable(`peer "studio" unreachable: dial timeout`))
	if c.peerUnavailable == "" {
		t.Fatal("WithPeerUnavailable did not record the reason")
	}

	result := &CloneResult{
		Success: true, Directory: "/campaigns/demo",
		Seed: []SeedRepoResult{{
			Repo: ".", Method: SeedMethodOrigin, Reason: c.peerUnavailable,
		}},
	}
	raw, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	var decoded JSONOutput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Seed == nil || len(decoded.Seed.Repos) != 1 {
		t.Fatalf("expected one seed entry reporting the fallback, got %s", raw)
	}
	if got := decoded.Seed.Repos[0]; got.Method != SeedMethodOrigin ||
		!strings.Contains(got.Reason, "unreachable") {
		t.Errorf("seed entry = %+v, want origin with an unreachable reason", got)
	}
}
