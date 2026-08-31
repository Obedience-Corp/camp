package compat

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

// TestOldStateCampaignYAMLLoads reads a campaign.yaml written before the camp
// vocabulary change and asserts every field still lands where camp expects it.
// The fixture is the contract: a renamed YAML key would parse to a zero value
// here rather than failing anywhere visible at runtime.
func TestOldStateCampaignYAMLLoads(t *testing.T) {
	cfg := parseOldStateCampaign(t)

	if cfg.ID != "8deed8b4-0000-4000-8000-0000000000aa" {
		t.Fatalf("id: got %q", cfg.ID)
	}
	if cfg.Name != "legacy-campaign" {
		t.Fatalf("name: got %q", cfg.Name)
	}
	if cfg.Type != config.CampaignTypeProduct {
		t.Fatalf("type: got %q", cfg.Type)
	}
	if cfg.Description == "" || cfg.Mission == "" {
		t.Fatalf("description/mission dropped: %q / %q", cfg.Description, cfg.Mission)
	}
	if cfg.CreatedAt.IsZero() {
		t.Fatal("created_at dropped; the fixture sets it")
	}
	if got := cfg.IntentTags(); len(got) != 2 || got[0] != "personal" {
		t.Fatalf("intents.tags: got %v", got)
	}
}

// TestOldStateCampaignProjectsSurvive pins the projects list, whose entries
// carry the shortcut map that `camp go` and `cgo` resolve against.
func TestOldStateCampaignProjectsSurvive(t *testing.T) {
	cfg := parseOldStateCampaign(t)

	if len(cfg.Projects) != 1 {
		t.Fatalf("projects: got %d entries, want 1", len(cfg.Projects))
	}
	p := cfg.Projects[0]
	if p.Name != "guild-core" || p.Path != "projects/guild-core" {
		t.Fatalf("project identity: got %q at %q", p.Name, p.Path)
	}
	if p.URL == "" || p.Branch != "main" {
		t.Fatalf("project url/branch: got %q / %q", p.URL, p.Branch)
	}
	if got := p.ResolveShortcut(""); got != "." {
		t.Fatalf("default shortcut: got %q, want %q", got, ".")
	}
	if got := p.ResolveShortcut("db"); got != "db/migrations" {
		t.Fatalf("named shortcut: got %q, want %q", got, "db/migrations")
	}
}

// TestOldStateConceptsPreserveOrder pins the explicit concept list, including
// nesting. Order is user-visible in the picker, so a reordering parse is a
// behavior change even when every concept survives.
func TestOldStateConceptsPreserveOrder(t *testing.T) {
	concepts := parseOldStateCampaign(t).Concepts()
	want := []string{"projects", "workflow", "docs"}
	if len(concepts) != len(want) {
		t.Fatalf("concepts: got %d, want %d", len(concepts), len(want))
	}
	for i, name := range want {
		if concepts[i].Name != name {
			t.Fatalf("concept %d: got %q, want %q", i, concepts[i].Name, name)
		}
	}
	if got := concepts[1].Children; len(got) != 2 || got[0].Name != "festivals" {
		t.Fatalf("nested concepts: got %v", got)
	}
	if concepts[0].Depth == nil || *concepts[0].Depth != 1 {
		t.Fatal("concept depth dropped")
	}
}

// TestOldStateJumpsPathsSurvive pins the navigation paths, including the two
// legacy collections (code_reviews, pipelines) that newer camps no longer
// scaffold but existing camps still contain.
func TestOldStateJumpsPathsSurvive(t *testing.T) {
	cfg := parseOldStateCampaign(t)

	paths := cfg.Paths()
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"projects", paths.Projects, "projects/"},
		{"worktrees", paths.Worktrees, "projects/worktrees/"},
		{"intents", paths.Intents, ".campaign/intents/"},
		{"code_reviews", paths.CodeReviews, "workflow/code_reviews/"},
		{"pipelines", paths.Pipelines, "workflow/pipelines/"},
		{"dungeon", paths.Dungeon, "dungeon/"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("paths.%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}

	shortcuts := cfg.Shortcuts()
	if got, ok := shortcuts["legacy-note"]; !ok || got.Source != "" {
		t.Fatalf("a shortcut written before the source field existed must load with an empty source: got %+v (present=%v)", got, ok)
	}
}

// The destructive-mistake guard — that loading this metadata and saving it back
// leaves it at .campaign/campaign.yaml and creates no .camp directory — needs a
// real workspace on a real filesystem, so it runs against the binary in
// TestCompatOldStateRichLayoutSurvivesRoundTrip
// (tests/integration/compat_oldstate_test.go) rather than here.
