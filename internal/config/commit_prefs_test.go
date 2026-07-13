package config

import "testing"

// Pure CommitPrefs logic lives here. The filesystem-backed resolution paths
// (EffectiveCommitPrefs reading global/local config, malformed-config failure,
// and the local.json round-trip) mutate real config files and therefore run in
// the containerized integration harness — see
// tests/integration/settings_commit_prefs_test.go.

func TestCommitPrefs_TagCommitsDefault(t *testing.T) {
	var p CommitPrefs
	if !p.TagCommits() {
		t.Fatal("default CommitPrefs should enable tags")
	}
	p.DisableCommitTags = true
	if p.TagCommits() {
		t.Fatal("DisableCommitTags should disable tags")
	}
}

func TestCommitPrefs_IsEmpty(t *testing.T) {
	if !(CommitPrefs{}).IsEmpty() {
		t.Fatal("zero CommitPrefs should be empty")
	}
	if (CommitPrefs{SyncProjectRefs: true}).IsEmpty() {
		t.Fatal("SyncProjectRefs set should not be empty")
	}
	if (CommitPrefs{DisableCommitTags: true}).IsEmpty() {
		t.Fatal("DisableCommitTags set should not be empty")
	}
}

func TestMergeCommitPrefs(t *testing.T) {
	global := CommitPrefs{SyncProjectRefs: true, DisableCommitTags: false}

	tests := []struct {
		name  string
		local *LocalSettings
		want  CommitPrefs
	}{
		{
			name:  "nil local keeps global",
			local: nil,
			want:  global,
		},
		{
			name:  "local without commit block keeps global",
			local: &LocalSettings{},
			want:  global,
		},
		{
			name:  "local commit block fully replaces global",
			local: &LocalSettings{Commit: &CommitPrefs{SyncProjectRefs: false, DisableCommitTags: true}},
			want:  CommitPrefs{SyncProjectRefs: false, DisableCommitTags: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeCommitPrefs(global, tt.local); got != tt.want {
				t.Fatalf("MergeCommitPrefs = %+v, want %+v", got, tt.want)
			}
		})
	}
}
