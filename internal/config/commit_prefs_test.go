package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

func TestEffectiveCommitPrefs_GlobalOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	cfg := DefaultGlobalConfig()
	cfg.Commit = CommitPrefs{SyncProjectRefs: true, DisableCommitTags: true}
	if err := SaveGlobalConfig(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	got := EffectiveCommitPrefs(context.Background(), "")
	if !got.SyncProjectRefs || !got.DisableCommitTags {
		t.Fatalf("global prefs not applied: %+v", got)
	}
}

func TestEffectiveCommitPrefs_LocalOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	cfg := DefaultGlobalConfig()
	cfg.Commit = CommitPrefs{SyncProjectRefs: true, DisableCommitTags: false}
	if err := SaveGlobalConfig(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(dir, "campaign")
	if err := os.MkdirAll(SettingsDirPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	local := &LocalSettings{Commit: &CommitPrefs{SyncProjectRefs: false, DisableCommitTags: true}}
	if err := SaveLocalSettings(context.Background(), root, local); err != nil {
		t.Fatal(err)
	}

	got := EffectiveCommitPrefs(context.Background(), root)
	if got.SyncProjectRefs {
		t.Fatalf("local should override sync to false: %+v", got)
	}
	if !got.DisableCommitTags {
		t.Fatalf("local should override tags off: %+v", got)
	}
}

func TestLocalSettings_CommitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "c")
	if err := os.MkdirAll(SettingsDirPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	in := &LocalSettings{Commit: &CommitPrefs{SyncProjectRefs: true}}
	if err := SaveLocalSettings(context.Background(), root, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadLocalSettings(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if out.Commit == nil || !out.Commit.SyncProjectRefs {
		t.Fatalf("round-trip lost commit prefs: %+v", out)
	}
	raw, err := os.ReadFile(LocalSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["commit"]; !ok {
		t.Fatalf("local.json missing commit key: %s", raw)
	}
}
