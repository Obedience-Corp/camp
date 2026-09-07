package fresh

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/project"
)

func TestBuildFreshWorkflowJSONGroupsTUISteps(t *testing.T) {
	off := false
	cfg := &config.FreshConfig{
		Prune: &off,
		FollowUp: []config.FollowUpConfig{
			{Name: "install", Run: "npm install", ContinueOnError: true},
		},
		Projects: map[string]config.FreshProjectConfig{
			"api": {Branch: ptr("feat/api")},
		},
	}

	payload := buildFreshWorkflowJSON(cfg, "api", []project.Project{{Name: "api"}, {Name: "web"}})
	if payload.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schema_version = %q, want %s", payload.SchemaVersion, JSONSchemaVersion)
	}
	if payload.Project != "api" {
		t.Fatalf("project = %q, want api", payload.Project)
	}
	if !payload.InheritsFollowUps {
		t.Fatal("api has no follow-up list of its own, want inherits_follow_ups")
	}

	if len(payload.Scopes) != 3 {
		t.Fatalf("scopes = %d, want 3 (global + api + web)", len(payload.Scopes))
	}
	if payload.Scopes[0].Name != "Global defaults" || payload.Scopes[0].Project != "" {
		t.Fatalf("first scope = %+v, want Global defaults", payload.Scopes[0])
	}
	if !payload.Scopes[1].Current || payload.Scopes[1].Overrides != 1 {
		t.Fatalf("api scope = %+v, want current with one override", payload.Scopes[1])
	}

	sections := map[string]int{}
	var prune, branch, follow *freshStepJSON
	for i := range payload.Steps {
		step := payload.Steps[i]
		sections[step.Section]++
		switch {
		case step.Setting == "prune":
			prune = &payload.Steps[i]
		case step.Setting == "branch":
			branch = &payload.Steps[i]
		case step.Follow != nil && step.Follow.Name == "install":
			follow = &payload.Steps[i]
		}
	}
	if sections["sync"] != 3 {
		t.Errorf("sync steps = %d, want 3", sections["sync"])
	}
	if sections["settings"] != 4 {
		t.Errorf("settings steps = %d, want 4", sections["settings"])
	}
	if sections["follow_ups"] != 1 {
		t.Errorf("follow-up steps = %d, want 1", sections["follow_ups"])
	}
	if prune == nil {
		t.Fatal("missing prune step")
	}
	if prune.Configurable {
		t.Fatal("prune must not be configurable in a project scope")
	}
	if prune.EditHint == "" {
		t.Fatal("project-scope prune should carry an edit hint")
	}
	if branch == nil {
		t.Fatal("missing branch step")
	}
	if branch.Stored != "branch" || branch.StoredBranch != "feat/api" {
		t.Fatalf("branch stored = %s/%s, want branch/feat/api", branch.Stored, branch.StoredBranch)
	}
	if len(branch.Options) != 3 {
		t.Fatalf("project branch options = %d, want inherit/no-branch/branch", len(branch.Options))
	}
	if follow == nil || !follow.Follow.ContinueOnError {
		t.Fatalf("follow-up = %+v, want install with continue_on_error", follow)
	}
}

func TestBuildFreshWorkflowJSONGlobalPruneIsEditable(t *testing.T) {
	payload := buildFreshWorkflowJSON(&config.FreshConfig{}, "", nil)
	var prune *freshStepJSON
	for i := range payload.Steps {
		if payload.Steps[i].Setting == "prune" {
			prune = &payload.Steps[i]
			break
		}
	}
	if prune == nil {
		t.Fatal("missing prune step")
	}
	if !prune.Configurable {
		t.Fatal("global prune should be configurable")
	}
	if prune.Stored != "inherit" {
		t.Fatalf("unconfigured prune stored = %q, want inherit", prune.Stored)
	}
	if len(prune.Options) != 3 {
		t.Fatalf("global prune options = %d, want default/on/off", len(prune.Options))
	}
}

func ptr(v string) *string { return &v }
