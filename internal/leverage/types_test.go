package leverage

import (
	"encoding/json"
	"testing"
)

// This is a subset of actual scc --format json2 output from the camp project.
// IMPORTANT: The field names must match exactly what scc produces.
const sampleSCCJSON = `{
	"languageSummary": [
		{
			"Name": "Go",
			"Bytes": 2242935,
			"CodeBytes": 0,
			"Lines": 85152,
			"Code": 65641,
			"Comment": 7193,
			"Blank": 12318,
			"Complexity": 15051,
			"Count": 392,
			"WeightedComplexity": 0,
			"ULOC": 0
		},
		{
			"Name": "Markdown",
			"Bytes": 36389,
			"CodeBytes": 0,
			"Lines": 1408,
			"Code": 1017,
			"Comment": 0,
			"Blank": 391,
			"Complexity": 0,
			"Count": 12,
			"WeightedComplexity": 0,
			"ULOC": 0
		}
	],
	"estimatedCost": 2251607.19,
	"estimatedScheduleMonths": 18.72,
	"estimatedPeople": 10.68
}`

func TestSCCResult_UnmarshalRealOutput(t *testing.T) {
	var result SCCResult
	if err := json.Unmarshal([]byte(sampleSCCJSON), &result); err != nil {
		t.Fatalf("Failed to unmarshal scc json2: %v", err)
	}

	if result.EstimatedPeople != 10.68 {
		t.Errorf("EstimatedPeople: want 10.68, got %f", result.EstimatedPeople)
	}
	if result.EstimatedScheduleMonths != 18.72 {
		t.Errorf("EstimatedScheduleMonths: want 18.72, got %f", result.EstimatedScheduleMonths)
	}
	if result.EstimatedCost != 2251607.19 {
		t.Errorf("EstimatedCost: want 2251607.19, got %f", result.EstimatedCost)
	}
	if len(result.LanguageSummary) != 2 {
		t.Fatalf("LanguageSummary: want 2 entries, got %d", len(result.LanguageSummary))
	}

	goEntry := result.LanguageSummary[0]
	if goEntry.Name != "Go" {
		t.Errorf("First language: want Go, got %s", goEntry.Name)
	}
	if goEntry.Lines != 85152 {
		t.Errorf("Go Lines: want 85152, got %d", goEntry.Lines)
	}
	if goEntry.Code != 65641 {
		t.Errorf("Go Code: want 65641, got %d", goEntry.Code)
	}
	if goEntry.Complexity != 15051 {
		t.Errorf("Go Complexity: want 15051, got %d", goEntry.Complexity)
	}
	if goEntry.Count != 392 {
		t.Errorf("Go Count: want 392, got %d", goEntry.Count)
	}
}

func TestLeverageConfig_RoundTrip(t *testing.T) {
	cfg := LeverageConfig{
		ActualPeople:      2,
		COCOMOProjectType: COCOMOOrganic,
		AvgWage:           70000,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var loaded LeverageConfig
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if loaded.ActualPeople != cfg.ActualPeople {
		t.Errorf("ActualPeople: want %d, got %d", cfg.ActualPeople, loaded.ActualPeople)
	}
	if loaded.COCOMOProjectType != cfg.COCOMOProjectType {
		t.Errorf("COCOMOProjectType: want %s, got %s", cfg.COCOMOProjectType, loaded.COCOMOProjectType)
	}
}
