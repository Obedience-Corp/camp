package workitem

import (
	"sort"
	"time"

	"github.com/Obedience-Corp/camp/internal/listview"
)

// SchemaVersion is the JSON contract version for workitem output.
//
// Changelog:
//   - v1alpha4: add optional .workitem metadata fields on each item
//     (stable_id, description, execution, priority_info, project, workflow,
//     lineage). All omitempty so legacy items without .workitem serialize
//     byte-identically except for this constant.
//   - v1alpha5: surface local .workflow/ runtime progress under workflow
//     (current_step, total_steps, completed_steps, run_status, blocked,
//     doc_hash_changed). All omitempty; populated only when
//     .workflow/workflow.yaml exists for the workitem directory.
//   - v1alpha6: publish per-type lifecycle stage vocabulary; use explicit
//     LifecycleStageNone ("none") for no-stage workitems; include ritual/ and
//     chains/ festivals; always emit workflow int/bool fields when workflow
//     metadata is present.
//   - v1alpha7: add attention_stage/attention_stage_source/group fields,
//     attention/group vocabularies, grouping metadata, and reusable section rows.
//   - v1alpha8: add config-derived workflow_category on each item, category to
//     available_group_by and section row fields, category_vocabulary, and
//     category_counts. workflow_category is omitempty; items are enriched at read
//     time, so unenriched payloads serialize byte-identically except for this
//     constant and the always-present category_vocabulary/category_counts.
const SchemaVersion = "workitems/v1alpha8"

// Payload is the top-level JSON output for camp workitem --json.
type Payload struct {
	SchemaVersion            string              `json:"schema_version"`
	GeneratedAt              time.Time           `json:"generated_at"`
	CampaignRoot             string              `json:"campaign_root"`
	Sort                     SortInfo            `json:"sort"`
	Grouping                 listview.Grouping   `json:"grouping"`
	Items                    []WorkItem           `json:"items"`
	Sections                 []listview.Section   `json:"sections,omitempty"`
	Counts                   Counts               `json:"counts"`
	CategoryCounts           map[string]int       `json:"category_counts"`
	StageVocabulary          map[string][]string  `json:"stage_vocabulary"`
	AttentionStageVocabulary []string             `json:"attention_stage_vocabulary"`
	GroupVocabulary          []string             `json:"group_vocabulary"`
	CategoryVocabulary       []CategoryVocabEntry `json:"category_vocabulary"`
}

// CategoryVocabEntry is a workflow category with display metadata, listed in the
// deterministic order returned by config (defaults first, then custom).
type CategoryVocabEntry struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
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
	return NewPayloadWithGrouping(campaignRoot, items, "attention_stage")
}

func NewPayloadWithGrouping(campaignRoot string, items []WorkItem, groupBy string) Payload {
	counts := Counts{Total: len(items)}
	categoryCounts := map[string]int{}
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
		if item.WorkflowCategory != "" {
			categoryCounts[item.WorkflowCategory]++
		}
	}

	// Ensure items is never null in JSON output
	if items == nil {
		items = []WorkItem{}
	}

	rows := ListRows(items)
	sections := listview.Sections(rows, groupBy)

	return Payload{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		Sort: SortInfo{
			Primary:   "manual_priority",
			Secondary: "sort_timestamp",
			Direction: "desc",
		},
		Grouping: listview.Grouping{
			GroupBy:          groupBy,
			AvailableGroupBy: []string{"attention_stage", "group", "type", "category"},
		},
		Items:                    items,
		Sections:                 sections,
		Counts:                   counts,
		CategoryCounts:           categoryCounts,
		StageVocabulary:          StageVocabulary(),
		AttentionStageVocabulary: []string{"current", "next", "active", "parked"},
		GroupVocabulary:          GroupVocabulary(items),
		CategoryVocabulary:       []CategoryVocabEntry{},
	}
}

func ListRows(items []WorkItem) []listview.Row {
	rows := make([]listview.Row, 0, len(items))
	for _, item := range items {
		groupKey := item.Group
		groupLabel := item.Group
		if groupKey == "" {
			groupKey = "ungrouped"
			groupLabel = "Ungrouped"
		}
		rows = append(rows, listview.Row{
			Key:        item.Key,
			Title:      item.Title,
			Path:       item.RelativePath,
			GroupKey:   groupKey,
			GroupLabel: groupLabel,
			StyleToken: "group:" + groupKey,
			Fields: map[string]string{
				"attention_stage": item.AttentionStage,
				"group":           groupKey,
				"type":            string(item.WorkflowType),
				"category":        item.WorkflowCategory,
			},
		})
	}
	return rows
}

func GroupVocabulary(items []WorkItem) []string {
	seen := map[string]bool{}
	for _, item := range items {
		if item.Group != "" {
			seen[item.Group] = true
		}
	}
	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}
