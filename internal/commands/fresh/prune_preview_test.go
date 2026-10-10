package fresh

import "testing"

func TestFreshSyncStatePruneBaseRef(t *testing.T) {
	onDefault := freshSyncState{defaultBranch: "main", baseRef: "main", displayRef: "main"}
	detached := freshSyncState{
		defaultBranch: "main",
		baseRef:       "origin/main",
		displayRef:    "origin/main (detached)",
		detached:      true,
	}
	reclaimed := freshSyncState{defaultBranch: "trunk", baseRef: "trunk", displayRef: "trunk", reclaimed: true}

	tests := []struct {
		name   string
		state  freshSyncState
		dryRun bool
		want   string
	}{
		// A real run prunes after the sync step, so the local default branch
		// is already level with origin and is the right base.
		{name: "real run on default branch", state: onDefault, want: "main"},
		// A dry-run never moved the local default branch; only origin/<default>
		// reflects what the sync step would produce.
		{name: "dry-run on default branch", state: onDefault, dryRun: true, want: "origin/main"},
		{name: "dry-run after reclaiming default branch", state: reclaimed, dryRun: true, want: "origin/trunk"},
		{name: "real run detached", state: detached, want: "origin/main"},
		{name: "dry-run detached", state: detached, dryRun: true, want: "origin/main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.pruneBaseRef(tt.dryRun); got != tt.want {
				t.Fatalf("pruneBaseRef(%v) = %q, want %q", tt.dryRun, got, tt.want)
			}
		})
	}
}
