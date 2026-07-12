package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestCampaignScalarsFrom(t *testing.T) {
	cfg := &config.CampaignConfig{
		Name:        "N",
		Description: "D",
		Mission:     "M",
		Type:        config.CampaignTypeTools,
		Hooks:       config.HooksConfig{CommitMessage: config.CommitMessageHookConfig{Command: "ob commit"}},
	}
	got := campaignScalarsFrom(cfg)
	want := campaignScalars{Name: "N", Description: "D", Mission: "M", Type: "tools", CommitCmd: "ob commit"}
	if got != want {
		t.Errorf("campaignScalarsFrom() = %+v, want %+v", got, want)
	}
}

func TestApplyCampaignScalars_PreservesOtherFields(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	depth := 2
	cfg := &config.CampaignConfig{
		ID:          "abc-123",
		Name:        "Old Name",
		Type:        config.CampaignTypeResearch,
		Description: "old desc",
		Mission:     "old mission",
		CreatedAt:   created,
		Projects:    []config.ProjectConfig{{Name: "p1", Path: "projects/p1"}},
		ConceptList: []config.ConceptEntry{{Name: "c1", Path: "x/", Depth: &depth}},
		Intents:     config.IntentsConfig{Tags: []string{"a", "b"}},
		Hooks:       config.HooksConfig{CommitMessage: config.CommitMessageHookConfig{Command: "old cmd"}},
	}

	applyCampaignScalars(cfg, campaignScalars{
		Name:        "New Name",
		Description: "new desc",
		Mission:     "new mission",
		Type:        string(config.CampaignTypeProduct),
		CommitCmd:   "new cmd",
	})

	// Scalars updated.
	if cfg.Name != "New Name" || cfg.Description != "new desc" || cfg.Mission != "new mission" {
		t.Errorf("scalars not updated: name=%q desc=%q mission=%q", cfg.Name, cfg.Description, cfg.Mission)
	}
	if cfg.Type != config.CampaignTypeProduct {
		t.Errorf("type = %q, want product", cfg.Type)
	}
	if cfg.Hooks.CommitMessage.Command != "new cmd" {
		t.Errorf("commit hook = %q, want %q", cfg.Hooks.CommitMessage.Command, "new cmd")
	}

	// Everything else preserved untouched.
	if cfg.ID != "abc-123" {
		t.Errorf("ID changed to %q", cfg.ID)
	}
	if !cfg.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt changed to %v", cfg.CreatedAt)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].Name != "p1" {
		t.Errorf("Projects changed: %+v", cfg.Projects)
	}
	if len(cfg.ConceptList) != 1 || cfg.ConceptList[0].Name != "c1" {
		t.Errorf("ConceptList changed: %+v", cfg.ConceptList)
	}
	if !reflect.DeepEqual(cfg.Intents.Tags, []string{"a", "b"}) {
		t.Errorf("Intents.Tags changed: %v", cfg.Intents.Tags)
	}
}

func TestParseTagLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty input yields nil", "", nil},
		{"only blank lines yields nil", "\n   \n\t\n", nil},
		{"trims, dedupes, preserves entry order", "  a \nb\n a \n\n c ", []string{"a", "b", "c"}},
		{"single tag", "personal", []string{"personal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTagLines(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTagLines(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateConcepts(t *testing.T) {
	tests := []struct {
		name    string
		entries []config.ConceptEntry
		wantErr bool
	}{
		{"missing name is rejected", []config.ConceptEntry{{Path: "x/"}}, true},
		{"leaf without path or children is rejected", []config.ConceptEntry{{Name: "orphan"}}, true},
		{"invalid child is rejected", []config.ConceptEntry{{Name: "workflow", Children: []config.ConceptEntry{{Path: "x/"}}}}, true},
		{"empty list is valid", nil, false},
		{"valid flat concept", []config.ConceptEntry{{Name: "projects", Path: "projects/"}}, false},
		{"parent may omit path when it has children", []config.ConceptEntry{{Name: "workflow", Children: []config.ConceptEntry{{Name: "design", Path: "workflow/design/"}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConcepts(tt.entries)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConcepts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
