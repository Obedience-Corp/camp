package workitem

import "time"

// SchemaVersion is the JSON contract version for workitem output.
const SchemaVersion = "workitems/v1alpha2"

// Payload is the top-level JSON output for camp workitem --json.
type Payload struct {
	SchemaVersion string     `json:"schema_version"`
	GeneratedAt   time.Time  `json:"generated_at"`
	CampaignRoot  string     `json:"campaign_root"`
	Sort          SortInfo   `json:"sort"`
	Items         []WorkItem `json:"items"`
	Counts        Counts     `json:"counts"`
}

// SortInfo describes the ordering applied to items.
type SortInfo struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Direction string `json:"direction"`
}

// Counts provides per-type item tallies.
type Counts struct {
	Total    int `json:"total"`
	Intent   int `json:"intent"`
	Design   int `json:"design"`
	Explore  int `json:"explore"`
	Festival int `json:"festival"`
}

// NewPayload builds a JSON-ready payload from a filtered, sorted item list.
func NewPayload(campaignRoot string, items []WorkItem) Payload {
	counts := Counts{Total: len(items)}
	for _, item := range items {
		switch item.WorkflowType {
		case WorkflowTypeIntent:
			counts.Intent++
		case WorkflowTypeDesign:
			counts.Design++
		case WorkflowTypeExplore:
			counts.Explore++
		case WorkflowTypeFestival:
			counts.Festival++
		}
	}

	// Ensure items is never null in JSON output
	if items == nil {
		items = []WorkItem{}
	}

	return Payload{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		Sort: SortInfo{
			Primary:   "sort_timestamp",
			Secondary: "created_at",
			Direction: "desc",
		},
		Items:  items,
		Counts: counts,
	}
}
