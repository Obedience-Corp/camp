package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCampaignConfigOmitsEmptyWorkflows(t *testing.T) {
	cfg := DefaultCampaignConfig("test")
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "workflows:") {
		t.Fatalf("empty workflows should be omitted from campaign.yaml, got:\n%s", data)
	}
}

func TestWorkflowCategoriesDefaults(t *testing.T) {
	c := &CampaignConfig{}
	got := c.WorkflowCategories()

	for _, key := range []string{WorkflowCategoryPlan, WorkflowCategoryResearch, WorkflowCategoryPipeline, WorkflowCategoryReview} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected default category %q present", key)
		}
	}
	if got[WorkflowCategoryReview].Label != "Review" {
		t.Fatalf("expected Review label, got %q", got[WorkflowCategoryReview].Label)
	}
}

func TestWorkflowCategoriesUserOverride(t *testing.T) {
	c := &CampaignConfig{
		Workflows: WorkflowsConfig{
			Categories: map[string]WorkflowCategoryConfig{
				WorkflowCategoryPlan: {Label: "Build", Description: "custom"},
				"ops":                {Label: "Ops"},
			},
		},
	}
	got := c.WorkflowCategories()

	if got[WorkflowCategoryPlan].Label != "Build" {
		t.Fatalf("expected user label to override default, got %q", got[WorkflowCategoryPlan].Label)
	}
	if got["ops"].Label != "Ops" {
		t.Fatalf("expected custom category present")
	}
	if got[WorkflowCategoryResearch].Label != "Research" {
		t.Fatalf("expected untouched default retained, got %q", got[WorkflowCategoryResearch].Label)
	}
}

func TestWorkflowCategoryForType(t *testing.T) {
	tests := []struct {
		name     string
		cfg      WorkflowsConfig
		typeKey  string
		expected string
	}{
		{"builtin design", WorkflowsConfig{}, "design", WorkflowCategoryPlan},
		{"builtin explore", WorkflowsConfig{}, "explore", WorkflowCategoryResearch},
		{"builtin code_reviews", WorkflowsConfig{}, "code_reviews", WorkflowCategoryReview},
		{"unknown type", WorkflowsConfig{}, "customthing", WorkflowCategoryUncategorized},
		{
			name:     "user override wins",
			cfg:      WorkflowsConfig{CategoryByType: map[string]string{"design": WorkflowCategoryResearch}},
			typeKey:  "design",
			expected: WorkflowCategoryResearch,
		},
		{
			name:     "user maps custom type",
			cfg:      WorkflowsConfig{CategoryByType: map[string]string{"customthing": WorkflowCategoryReview}},
			typeKey:  "customthing",
			expected: WorkflowCategoryReview,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &CampaignConfig{Workflows: tt.cfg}
			if got := c.WorkflowCategoryForType(tt.typeKey); got != tt.expected {
				t.Fatalf("WorkflowCategoryForType(%q) = %q, want %q", tt.typeKey, got, tt.expected)
			}
		})
	}
}

func TestOrderedWorkflowCategoryKeys(t *testing.T) {
	c := &CampaignConfig{
		Workflows: WorkflowsConfig{
			Categories: map[string]WorkflowCategoryConfig{
				"ops":   {Label: "Ops"},
				"admin": {Label: "Admin"},
			},
		},
	}
	got := c.OrderedWorkflowCategoryKeys()
	want := []string{WorkflowCategoryPlan, WorkflowCategoryResearch, WorkflowCategoryPipeline, WorkflowCategoryReview, "admin", "ops"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OrderedWorkflowCategoryKeys() = %v, want %v", got, want)
	}
}

func TestValidateWorkflowsConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *WorkflowsConfig
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", &WorkflowsConfig{}, false},
		{
			name:    "valid mapping to default category",
			cfg:     &WorkflowsConfig{CategoryByType: map[string]string{"customthing": WorkflowCategoryResearch}},
			wantErr: false,
		},
		{
			name: "valid mapping to user category",
			cfg: &WorkflowsConfig{
				Categories:     map[string]WorkflowCategoryConfig{"ops": {Label: "Ops"}},
				CategoryByType: map[string]string{"deploy": "ops"},
			},
			wantErr: false,
		},
		{
			name:    "mapping to unknown category",
			cfg:     &WorkflowsConfig{CategoryByType: map[string]string{"deploy": "nope"}},
			wantErr: true,
		},
		{
			name:    "invalid category key",
			cfg:     &WorkflowsConfig{Categories: map[string]WorkflowCategoryConfig{"bad/key": {Label: "x"}}},
			wantErr: true,
		},
		{
			name:    "invalid type key",
			cfg:     &WorkflowsConfig{CategoryByType: map[string]string{"bad key": WorkflowCategoryPlan}},
			wantErr: true,
		},
		{
			name:    "empty mapped category",
			cfg:     &WorkflowsConfig{CategoryByType: map[string]string{"deploy": ""}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowsConfig(tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
