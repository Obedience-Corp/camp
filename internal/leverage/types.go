// Package leverage computes productivity leverage scores by comparing
// scc COCOMO estimates against actual development effort.
package leverage

import (
	"context"
	"time"
)

const (
	// FormatJSON2 is the scc output format that includes COCOMO estimates.
	FormatJSON2 = "json2"

	// COCOMOOrganic is the most conservative COCOMO project type.
	COCOMOOrganic = "organic"

	// DefaultAvgWage is the default average yearly salary in USD
	// used by scc for cost estimation.
	DefaultAvgWage = 56286.0
)

// Runner executes scc on a directory and returns parsed results.
// The interface exists so tests can inject a mock without requiring
// the scc binary to be installed.
type Runner interface {
	Run(ctx context.Context, dir string) (*SCCResult, error)
}

// SCCResult is the top-level JSON object returned by `scc --format json2`.
//
// Verified against scc v3.6.0 output. The exact JSON field names are:
//
//	{
//	  "languageSummary": [...],
//	  "estimatedCost": 2251607.19,
//	  "estimatedScheduleMonths": 18.72,
//	  "estimatedPeople": 10.68
//	}
//
// Note: scc does NOT output an "estimatedEffort" field. The people count
// is provided directly by scc as estimatedPeople.
type SCCResult struct {
	LanguageSummary         []LanguageEntry `json:"languageSummary"`
	EstimatedCost           float64         `json:"estimatedCost"`
	EstimatedScheduleMonths float64         `json:"estimatedScheduleMonths"`
	EstimatedPeople         float64         `json:"estimatedPeople"`
}

// LanguageEntry is one element of the languageSummary array in scc json2 output.
//
// Verified field names against scc v3.6.0. Note that "Name", "Lines", "Code"
// etc. are PascalCase in the JSON output (scc convention).
type LanguageEntry struct {
	Name               string `json:"Name"`
	Bytes              int    `json:"Bytes"`
	CodeBytes          int    `json:"CodeBytes"`
	Lines              int    `json:"Lines"`
	Code               int    `json:"Code"`
	Comment            int    `json:"Comment"`
	Blank              int    `json:"Blank"`
	Complexity         int    `json:"Complexity"`
	Count              int    `json:"Count"`
	WeightedComplexity int    `json:"WeightedComplexity"`
	ULOC               int    `json:"ULOC"`
}

// LeverageScore holds the computed leverage metrics for one project
// or an aggregate across all projects.
type LeverageScore struct {
	// ProjectName identifies the project. Empty for campaign-wide aggregate.
	ProjectName string `json:"project_name,omitempty"`

	// Inputs from scc
	EstimatedPeople float64 `json:"estimated_people"`
	EstimatedMonths float64 `json:"estimated_months"`
	EstimatedCost   float64 `json:"estimated_cost"`

	// Inputs from config / actual effort
	ActualPeople  float64 `json:"actual_people"`
	ElapsedMonths float64 `json:"elapsed_months"`

	// Computed scores
	// FullLeverage = (EstimatedPeople * EstimatedMonths) / (ActualPeople * ElapsedMonths)
	FullLeverage float64 `json:"full_leverage"`
	// SimpleLeverage = EstimatedPeople / ActualPeople
	SimpleLeverage float64 `json:"simple_leverage"`

	// Summary stats from scc
	TotalLines int `json:"total_lines"`
	TotalCode  int `json:"total_code"`
}

// LeverageConfig is the schema for .campaign/leverage/config.json.
type LeverageConfig struct {
	// ActualPeople is the number of developers working on the campaign.
	ActualPeople int `json:"actual_people"`

	// ProjectStart is when development began (used to compute elapsed months).
	ProjectStart time.Time `json:"project_start"`

	// COCOMOProjectType controls the COCOMO model variant (default: "organic").
	COCOMOProjectType string `json:"cocomo_project_type,omitempty"`

	// AvgWage overrides the average yearly salary for cost estimation.
	AvgWage float64 `json:"avg_wage,omitempty"`
}
