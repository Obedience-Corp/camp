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

// WorkflowsConfig is the campaign workflow-category taxonomy stored under the
// `workflows:` block in .campaign/campaign.yaml. Categories classify workflow
// and workitem type keys into broad families so camp workitem, the TUI, and
// downstream consumers can filter and group by kind of work without changing
// the workitem type contract.
type WorkflowsConfig struct {
	Categories     map[string]WorkflowCategoryConfig `yaml:"categories,omitempty"`
	CategoryByType map[string]string                 `yaml:"category_by_type,omitempty"`
}

// WorkflowCategoryConfig is the display metadata for a single category.
type WorkflowCategoryConfig struct {
	Label       string `yaml:"label,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// DefaultWorkflowCategories returns the built-in category vocabulary. The set is
// intentionally generic so any custom workflow type can be mapped into one of it.
func DefaultWorkflowCategories() map[string]WorkflowCategoryConfig {
	return map[string]WorkflowCategoryConfig{
		WorkflowCategoryPlan:     {Label: "Plan", Description: "Planning, design, intents, festivals, and structured execution work"},
		WorkflowCategoryResearch: {Label: "Research", Description: "Exploration, comparison, discovery, and investigation"},
		WorkflowCategoryPipeline: {Label: "Pipeline", Description: "Structured campaign data movement, compilation, ingestion, or export"},
		WorkflowCategoryReview:   {Label: "Review", Description: "Review-style work such as code reviews and quality passes"},
	}
}

// DefaultWorkflowCategoryByType returns the built-in type->category mapping.
// Common `camp workitem create` types (feature, bug, chore) are included so
// freshly created items are not uncategorized.
func DefaultWorkflowCategoryByType() map[string]string {
	return map[string]string{
		"intent":       WorkflowCategoryPlan,
		"design":       WorkflowCategoryPlan,
		"explore":      WorkflowCategoryResearch,
		"festival":     WorkflowCategoryPlan,
		"feature":      WorkflowCategoryPlan,
		"bug":          WorkflowCategoryPlan,
		"chore":        WorkflowCategoryPlan,
		"code_reviews": WorkflowCategoryReview,
		"review":       WorkflowCategoryReview,
		"pipeline":     WorkflowCategoryPipeline,
		"pipelines":    WorkflowCategoryPipeline,
		"research":     WorkflowCategoryResearch,
		"experiment":   WorkflowCategoryResearch,
	}
}

// DefaultWorkflowsConfig returns the workflows block written into a fresh
// campaign.yaml (and backfilled by init --repair). It ships the full category
// vocabulary plus explicit mappings for camp's own built-in types so the block
// is a readable, editable starting point. Custom types default to
// WorkflowCategoryUncategorized via WorkflowCategoryForType until mapped.
func DefaultWorkflowsConfig() WorkflowsConfig {
	return WorkflowsConfig{
		Categories: DefaultWorkflowCategories(),
		CategoryByType: map[string]string{
			"intent":       WorkflowCategoryPlan,
			"design":       WorkflowCategoryPlan,
			"explore":      WorkflowCategoryResearch,
			"festival":     WorkflowCategoryPlan,
			"code_reviews": WorkflowCategoryReview,
			"pipelines":    WorkflowCategoryPipeline,
		},
	}
}

// WorkflowCategories returns the effective category vocabulary: built-in
// defaults merged with, and overridden by, user categories. It never mutates
// the loaded config.
func (c *CampaignConfig) WorkflowCategories() map[string]WorkflowCategoryConfig {
	merged := DefaultWorkflowCategories()
	maps.Copy(merged, c.Workflows.Categories)
	return merged
}

// WorkflowCategoryForType returns the category key for a workflow type. User
// mappings take precedence over defaults; unmapped types return
// WorkflowCategoryUncategorized.
func (c *CampaignConfig) WorkflowCategoryForType(workflowType string) string {
	if cat, ok := c.Workflows.CategoryByType[workflowType]; ok && cat != "" {
		return cat
	}
	if cat, ok := DefaultWorkflowCategoryByType()[workflowType]; ok {
		return cat
	}
	return WorkflowCategoryUncategorized
}

// OrderedWorkflowCategoryKeys returns category keys in a deterministic order:
// the built-in defaults first (plan, research, pipeline, review), then any
// custom categories alphabetically.
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

// ValidateWorkflowsConfig validates the workflows block. Category keys and
// mapped type keys must be slug-safe (same contract as workitem types). Each
// category_by_type value must resolve to a category that exists in the effective
// (default plus user) vocabulary. Unknown type keys are allowed so a type can be
// mapped before any matching workitem exists.
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
