package fresh

import (
	"context"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/project"
)

func TestConfiguredProjectNamesOnlyIncludesFollowUpOverrides(t *testing.T) {
	cfg := &config.FreshConfig{
		Projects: map[string]config.FreshProjectConfig{
			"zeta":  {FollowUp: []config.FollowUpConfig{{Name: "build"}}},
			"empty": {FollowUp: []config.FollowUpConfig{}},
			"unset": {},
			"alpha": {Branch: stringPtr("develop")},
		},
	}

	got := configuredProjectNames(cfg)
	want := []string{"empty", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("configuredProjectNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("configuredProjectNames() = %v, want %v", got, want)
		}
	}
}

func TestFollowUpsForScope(t *testing.T) {
	cfg := &config.FreshConfig{
		FollowUp: []config.FollowUpConfig{{Name: "install"}},
		Projects: map[string]config.FreshProjectConfig{
			"camp": {FollowUp: []config.FollowUpConfig{{Name: "build"}}},
		},
	}

	if got := followUpsForScope(cfg, globalFollowUpScope); len(got) != 1 || got[0].Name != "install" {
		t.Fatalf("global follow-ups = %v, want install", got)
	}
	if got := followUpsForScope(cfg, "camp"); len(got) != 1 || got[0].Name != "build" {
		t.Fatalf("camp follow-ups = %v, want build", got)
	}
}

func TestScopeHelpers(t *testing.T) {
	if got := scopeProjectName(globalFollowUpScope); got != "" {
		t.Fatalf("scopeProjectName(global) = %q, want empty", got)
	}
	if got := scopeProjectName("camp"); got != "camp" {
		t.Fatalf("scopeProjectName(camp) = %q, want camp", got)
	}
	if got := scopeDescription(globalFollowUpScope); got != "the global default" {
		t.Fatalf("scopeDescription(global) = %q", got)
	}
	if got := scopeDescription("camp"); got != "project camp" {
		t.Fatalf("scopeDescription(camp) = %q", got)
	}
}

func TestRequiredField(t *testing.T) {
	validate := requiredField("command")
	if err := validate("  "); err == nil {
		t.Fatal("requiredField accepted whitespace-only input")
	}
	if err := validate("npm install"); err != nil {
		t.Fatalf("requiredField rejected valid input: %v", err)
	}
}

func TestFollowUpTUIModelVisualizesResolvedWorkflow(t *testing.T) {
	cfg := &config.FreshConfig{
		Branch:   "work",
		FollowUp: []config.FollowUpConfig{{Name: "install", Run: "npm install"}},
		Projects: map[string]config.FreshProjectConfig{
			"web-app": {FollowUp: []config.FollowUpConfig{{Name: "build", Run: "npm run build"}}},
		},
	}
	projects := []project.Project{{Name: "web-app"}, {Name: "camp"}}
	m := newFollowUpTUIModel(context.Background(), "/campaign", projects, cfg)

	if got := m.scopes[0].label; got != "Global defaults" {
		t.Fatalf("global scope label = %q", got)
	}
	if len(m.workflowSteps()) != 9 {
		t.Fatalf("global workflow steps = %d, want 9", len(m.workflowSteps()))
	}

	m.rebuildScopes("web-app")
	steps := m.workflowSteps()
	if got := workflowScopeLabel(scopeProjectName(m.selectedScope())); got != "project web-app" {
		t.Fatalf("selected scope = %q", got)
	}
	if len(steps) != 9 || steps[7].Follow == nil || steps[7].Follow.Name != "build" {
		t.Fatalf("project workflow = %+v, want resolved build follow-up", steps)
	}
}

func TestFailureBehaviorLabel(t *testing.T) {
	if got := failureBehaviorLabel(false); got != "Stop fresh if this command fails" {
		t.Fatalf("failureBehaviorLabel(false) = %q", got)
	}
	if got := failureBehaviorLabel(true); got != "Continue to later steps if this command fails" {
		t.Fatalf("failureBehaviorLabel(true) = %q", got)
	}
}

func stringPtr(value string) *string { return &value }
