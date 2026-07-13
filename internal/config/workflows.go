package config

import (
	"maps"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

const (
	WorkflowCategoryPlan     = "plan"
	WorkflowCategoryResearch = "research"
	WorkflowCategoryPipeline = "pipeline"
	WorkflowCategoryReview   = "review"

	WorkflowCategoryUncategorized = "uncategorized"
)

type WorkflowsConfig struct {
	Categories     map[string]WorkflowCategoryConfig `yaml:"categories,omitempty"`
	CategoryByType map[string]string                 `yaml:"category_by_type,omitempty"`
}

type WorkflowCategoryConfig struct {
	Label       string `yaml:"label,omitempty"`
	Description string `yaml:"description,omitempty"`
}

func DefaultWorkflowCategories() map[string]WorkflowCategoryConfig {
	return map[string]WorkflowCategoryConfig{
		WorkflowCategoryPlan:     {Label: "Plan", Description: "Planning, design, intents, festivals, and structured execution work"},
		WorkflowCategoryResearch: {Label: "Research", Description: "Exploration, comparison, discovery, and investigation"},
		WorkflowCategoryPipeline: {Label: "Pipeline", Description: "Structured campaign data movement, compilation, ingestion, or export"},
		WorkflowCategoryReview:   {Label: "Review", Description: "Review-style work such as code reviews and quality passes"},
	}
}

func DefaultWorkflowCategoryByType() map[string]string {
	return map[string]string{
		"intent":       WorkflowCategoryPlan,
		"design":       WorkflowCategoryPlan,
		"explore":      WorkflowCategoryResearch,
		"festival":     WorkflowCategoryPlan,
		"reviews":      WorkflowCategoryReview,
		"code_reviews": WorkflowCategoryReview,
		"pipelines":    WorkflowCategoryPipeline,
	}
}

func DefaultWorkflowsConfig() WorkflowsConfig {
	return WorkflowsConfig{
		Categories:     DefaultWorkflowCategories(),
		CategoryByType: DefaultWorkflowCategoryByType(),
	}
}

func (c *CampaignConfig) WorkflowCategories() map[string]WorkflowCategoryConfig {
	merged := DefaultWorkflowCategories()
	maps.Copy(merged, c.Workflows.Categories)
	return merged
}

func (c *CampaignConfig) WorkflowCategoryForType(workflowType string) string {
	if cat, ok := c.Workflows.CategoryByType[workflowType]; ok && cat != "" {
		return cat
	}
	if cat, ok := DefaultWorkflowCategoryByType()[workflowType]; ok {
		return cat
	}
	return WorkflowCategoryUncategorized
}

func (c *CampaignConfig) OrderedWorkflowCategoryKeys() []string {
	ordered := []string{WorkflowCategoryPlan, WorkflowCategoryResearch, WorkflowCategoryPipeline, WorkflowCategoryReview}
	seen := make(map[string]bool, len(ordered))
	for _, k := range ordered {
		seen[k] = true
	}
	var custom []string
	for k := range c.WorkflowCategories() {
		if !seen[k] {
			custom = append(custom, k)
		}
	}
	sort.Strings(custom)
	return append(ordered, custom...)
}

func ValidateWorkflowsConfig(cfg *WorkflowsConfig) error {
	if cfg == nil {
		return nil
	}

	for _, key := range sortedKeys(cfg.Categories) {
		if err := pathutil.ValidateSegment("workflow category", key); err != nil {
			return err
		}
	}

	effective := DefaultWorkflowCategories()
	maps.Copy(effective, cfg.Categories)

	for _, typeKey := range sortedKeys(cfg.CategoryByType) {
		if err := pathutil.ValidateSegment("workflow type", typeKey); err != nil {
			return err
		}
		cat := cfg.CategoryByType[typeKey]
		if err := pathutil.ValidateSegment("workflow category", cat); err != nil {
			return err
		}
		if _, ok := effective[cat]; !ok {
			return camperrors.NewValidation("category_by_type",
				"type "+typeKey+" maps to unknown category "+cat, nil)
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
