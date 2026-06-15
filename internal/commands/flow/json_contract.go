package flow

import (
	"time"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

// WorkflowItemsSchemaVersion is the JSON contract for camp flow items --json.
// It is separate from workflow/v1 because this payload is grouped filesystem
// item inventory, not the campaign workflow collection payload.
//
// Changelog:
//   - workflow-items/v1alpha1: initial versioned contract (N-7 fix)
const WorkflowItemsSchemaVersion = "workflow-items/v1alpha1"

type FlowItemsPayload struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Items         []FlowStatusItem `json:"items"`
}

type FlowStatusItem struct {
	Status  string          `json:"status"`
	Entries []workflow.Item `json:"entries"`
}
