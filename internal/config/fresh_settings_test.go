package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, CampaignDir, SettingsDir), 0o755); err != nil {
		t.Fatalf("create settings dir: %v", err)
	}
	return root
}

func loadSettings(t *testing.T, root string) *FreshConfig {
	t.Helper()
	cfg, err := LoadFreshConfig(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadFreshConfig: %v", err)
	}
	return cfg
}

func readSettings(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(FreshConfigPath(root))
	if err != nil {
		t.Fatalf("read fresh.yaml: %v", err)
	}
	return string(raw)
}

func TestSetFreshBranchRoundTrips(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	develop := "develop"
	if err := SetFreshBranch(ctx, root, "", &develop); err != nil {
		t.Fatalf("set global branch: %v", err)
	}
	if got := loadSettings(t, root).Branch; got != "develop" {
		t.Fatalf("global branch = %q, want develop", got)
	}

	feature := "feat/api"
	if err := SetFreshBranch(ctx, root, "api", &feature); err != nil {
		t.Fatalf("set project branch: %v", err)
	}
	cfg := loadSettings(t, root)
	if cfg.Projects["api"].Branch == nil || *cfg.Projects["api"].Branch != "feat/api" {
		t.Fatalf("project branch = %v, want feat/api", cfg.Projects["api"].Branch)
	}
	if got := cfg.ResolveFreshBranch("", false, "api"); got != "feat/api" {
		t.Errorf("resolved branch = %q, want the project override", got)
	}
}

// A project that explicitly wants no branch has to survive a global branch
// being configured later, which is why the empty string is written rather than
// the key being dropped.
func TestSetFreshBranchDistinguishesEmptyFromCleared(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	develop := "develop"
	if err := SetFreshBranch(ctx, root, "", &develop); err != nil {
		t.Fatalf("set global branch: %v", err)
	}

	empty := ""
	if err := SetFreshBranch(ctx, root, "api", &empty); err != nil {
		t.Fatalf("set empty project branch: %v", err)
	}
	cfg := loadSettings(t, root)
	if cfg.Projects["api"].Branch == nil {
		t.Fatal("explicit empty branch was dropped instead of written")
	}
	if got := cfg.ResolveFreshBranch("", false, "api"); got != "" {
		t.Errorf("resolved branch = %q, want no branch", got)
	}

	if err := SetFreshBranch(ctx, root, "api", nil); err != nil {
		t.Fatalf("clear project branch: %v", err)
	}
	if got := loadSettings(t, root).ResolveFreshBranch("", false, "api"); got != "develop" {
		t.Errorf("resolved branch after clearing = %q, want the inherited develop", got)
	}
}

// The global key has no inherit/empty distinction: yaml omits an empty string,
// so both mean "no branch" and neither should leave a stray key behind.
func TestSetFreshBranchClearsGlobalKey(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	develop := "develop"
	if err := SetFreshBranch(ctx, root, "", &develop); err != nil {
		t.Fatalf("set global branch: %v", err)
	}
	empty := ""
	if err := SetFreshBranch(ctx, root, "", &empty); err != nil {
		t.Fatalf("clear global branch: %v", err)
	}

	if strings.Contains(readSettings(t, root), "branch:") {
		t.Errorf("global branch key survived being cleared:\n%s", readSettings(t, root))
	}
}

func TestSetFreshBoolsRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)
	off := false

	if err := SetFreshPrune(ctx, root, &off); err != nil {
		t.Fatalf("set prune: %v", err)
	}
	if err := SetFreshPruneRemote(ctx, root, &off); err != nil {
		t.Fatalf("set prune_remote: %v", err)
	}
	if err := SetFreshPushUpstream(ctx, root, "api", &off); err != nil {
		t.Fatalf("set project push_upstream: %v", err)
	}

	cfg := loadSettings(t, root)
	if cfg.ResolveFreshPrune() {
		t.Error("prune resolved true after being set false")
	}
	if cfg.ResolveFreshPruneRemote() {
		t.Error("prune_remote resolved true after being set false")
	}
	if cfg.ResolveFreshPushUpstream("api") {
		t.Error("project push_upstream resolved true after being set false")
	}
	if !cfg.ResolveFreshPushUpstream("other") {
		t.Error("an unrelated project lost the default push_upstream")
	}
}

func TestSetFreshPushUpstreamNilRestoresInheritance(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	off := false
	if err := SetFreshPushUpstream(ctx, root, "", &off); err != nil {
		t.Fatalf("set global push_upstream: %v", err)
	}
	on := true
	if err := SetFreshPushUpstream(ctx, root, "api", &on); err != nil {
		t.Fatalf("set project push_upstream: %v", err)
	}
	if !loadSettings(t, root).ResolveFreshPushUpstream("api") {
		t.Fatal("project override did not take effect")
	}

	if err := SetFreshPushUpstream(ctx, root, "api", nil); err != nil {
		t.Fatalf("clear project push_upstream: %v", err)
	}
	if loadSettings(t, root).ResolveFreshPushUpstream("api") {
		t.Error("cleared project key did not fall back to the global false")
	}
}

// Clearing a project's last key should not leave an empty projects.<name>
// mapping, which would show up in the TUI as a project that overrides nothing.
func TestClearingLastProjectKeyPrunesTheScope(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	branch := "feat/api"
	if err := SetFreshBranch(ctx, root, "api", &branch); err != nil {
		t.Fatalf("set project branch: %v", err)
	}
	if err := SetFreshBranch(ctx, root, "api", nil); err != nil {
		t.Fatalf("clear project branch: %v", err)
	}

	raw := readSettings(t, root)
	if strings.Contains(raw, "projects:") || strings.Contains(raw, "api") {
		t.Errorf("emptied project scope survived:\n%s", raw)
	}
	if len(loadSettings(t, root).Projects) != 0 {
		t.Error("emptied project scope still parses as a configured project")
	}
}

// A project with follow-ups keeps its scope when a different key is cleared.
func TestClearingOneKeyKeepsOtherProjectKeys(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	if err := AddFreshFollowUp(ctx, root, "api", FollowUpConfig{Name: "gen", Run: "just gen"}); err != nil {
		t.Fatalf("add follow-up: %v", err)
	}
	branch := "feat/api"
	if err := SetFreshBranch(ctx, root, "api", &branch); err != nil {
		t.Fatalf("set project branch: %v", err)
	}
	if err := SetFreshBranch(ctx, root, "api", nil); err != nil {
		t.Fatalf("clear project branch: %v", err)
	}

	cfg := loadSettings(t, root)
	if got := cfg.Projects["api"].FollowUp; len(got) != 1 || got[0].Name != "gen" {
		t.Fatalf("project follow-ups = %+v, want the gen step to survive", got)
	}
	if cfg.Projects["api"].Branch != nil {
		t.Error("cleared branch key survived")
	}
}

// The scaffolded fresh.yaml ships fully commented out. Writing a setting to it
// must not discard that documentation.
func TestSetFreshSettingPreservesFileComments(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)
	header := "# fresh.yaml - post-merge branch cycling\n# branch: develop\n"
	if err := os.WriteFile(FreshConfigPath(root), []byte(header), 0o644); err != nil {
		t.Fatalf("seed fresh.yaml: %v", err)
	}

	branch := "develop"
	if err := SetFreshBranch(ctx, root, "", &branch); err != nil {
		t.Fatalf("set branch: %v", err)
	}

	raw := readSettings(t, root)
	if !strings.Contains(raw, "# fresh.yaml - post-merge branch cycling") {
		t.Errorf("documentation header was discarded:\n%s", raw)
	}
	if got := loadSettings(t, root).Branch; got != "develop" {
		t.Errorf("branch = %q, want develop", got)
	}
}

// Rewriting a key must replace its value rather than append a duplicate, which
// yaml would resolve to the last occurrence and quietly double the file.
func TestSetFreshSettingReplacesExistingValue(t *testing.T) {
	ctx := context.Background()
	root := settingsRoot(t)

	first, second := "one", "two"
	if err := SetFreshBranch(ctx, root, "", &first); err != nil {
		t.Fatalf("set branch: %v", err)
	}
	if err := SetFreshBranch(ctx, root, "", &second); err != nil {
		t.Fatalf("rewrite branch: %v", err)
	}

	raw := readSettings(t, root)
	if strings.Count(raw, "branch:") != 1 {
		t.Errorf("branch key written twice:\n%s", raw)
	}
	if got := loadSettings(t, root).Branch; got != "two" {
		t.Errorf("branch = %q, want two", got)
	}
}

func TestProjectOverrideKeysCountsEveryKey(t *testing.T) {
	branch := "feat/api"
	on := true

	tests := []struct {
		name string
		pc   FreshProjectConfig
		want int
	}{
		{name: "empty", pc: FreshProjectConfig{}, want: 0},
		{name: "branch only", pc: FreshProjectConfig{Branch: &branch}, want: 1},
		{name: "follow-ups only", pc: FreshProjectConfig{FollowUp: []FollowUpConfig{{Name: "gen"}}}, want: 1},
		{
			name: "explicitly empty follow-ups still overrides",
			pc:   FreshProjectConfig{FollowUp: []FollowUpConfig{}},
			want: 1,
		},
		{
			name: "every key",
			pc:   FreshProjectConfig{Branch: &branch, PushUpstream: &on, FollowUp: []FollowUpConfig{{Name: "gen"}}},
			want: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProjectOverrideKeys(tt.pc); got != tt.want {
				t.Errorf("ProjectOverrideKeys() = %d, want %d", got, tt.want)
			}
		})
	}
}
