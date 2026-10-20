package workspace

import (
	"encoding/json"
	"time"
)

const (
	// SchemaVersion is the public JSON contract for show/validate/push results.
	SchemaVersion = "quest-workspace/v1alpha1"

	// DocumentVersion is the on-disk ui-state.json version (festival-app v2).
	DocumentVersion = 2

	// FileName is the sidecar layout file inside a quest directory.
	FileName = "ui-state.json"

	// LockFileName is the cooperating-writer OS lock beside ui-state.json.
	LockFileName = "ui-state.lock"

	// OriginAgent marks tabs owned by Camp offline push.
	OriginAgent = "agent"

	// OriginUser marks interactive user tabs when explicitly stamped.
	OriginUser = "user"

	// DefaultUserTabID matches festival-app DEFAULT_WORKSPACE_TAB_ID for
	// legacy root-paneTree → workspaceTabs migration.
	DefaultUserTabID = "workspace-tab-1"

	// DefaultUserTabTitle matches festival-app DEFAULT_WORKSPACE_TAB_TITLE.
	DefaultUserTabTitle = "Tab 1"

	// DefaultLockWait matches festival-app ui_state_merge.rs (5s).
	DefaultLockWait = 5 * time.Second
)

// ShowResult is the quest-workspace/v1alpha1 success payload for show --json.
type ShowResult struct {
	SchemaVersion string          `json:"schema_version"`
	CampaignRoot  string          `json:"campaign_root"`
	QuestID       string          `json:"quest_id"`
	QuestSlug     string          `json:"quest_slug"`
	Path          string          `json:"path"`
	Generation    string          `json:"generation"`
	Document      json.RawMessage `json:"document"`
}

// ValidateResult is the quest-workspace/v1alpha1 success payload for validate --json.
type ValidateResult struct {
	SchemaVersion string `json:"schema_version"`
	CampaignRoot  string `json:"campaign_root"`
	QuestID       string `json:"quest_id"`
	QuestSlug     string `json:"quest_slug"`
	Path          string `json:"path"`
	Valid         bool   `json:"valid"`
	Generation    string `json:"generation,omitempty"`
}

// PushResult is the quest-workspace/v1alpha1 success payload for push --json.
type PushResult struct {
	SchemaVersion string          `json:"schema_version"`
	CampaignRoot  string          `json:"campaign_root"`
	QuestID       string          `json:"quest_id"`
	QuestSlug     string          `json:"quest_slug"`
	Path          string          `json:"path"`
	TabID         string          `json:"tab_id"`
	Action        string          `json:"action"` // "appended" or "replaced"
	Generation    string          `json:"generation"`
	Document      json.RawMessage `json:"document"`
}

// PushSpec is the agent-supplied tab description accepted by push (--file/stdin).
// Camp stamps origin=agent. Runtime-only pane bindings are stripped before write.
type PushSpec struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	PaneTree     json.RawMessage `json:"paneTree"`
	OpenFiles    json.RawMessage `json:"openFiles,omitempty"`
	CurrentPath  *string         `json:"currentPath,omitempty"`
	ActivePaneID *string         `json:"activePaneId,omitempty"`
	ChainConfig  json.RawMessage `json:"chainConfig,omitempty"`
	Extra        map[string]json.RawMessage
}

// UnmarshalJSON preserves unknown top-level keys for round-trip metadata.
func (s *PushSpec) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Extra = map[string]json.RawMessage{}
	known := map[string]bool{
		"id": true, "title": true, "paneTree": true, "openFiles": true,
		"currentPath": true, "activePaneId": true, "chainConfig": true,
		"origin": true, // ignored; Camp always stamps agent
	}
	if v, ok := raw["id"]; ok {
		_ = json.Unmarshal(v, &s.ID)
	}
	if v, ok := raw["title"]; ok {
		_ = json.Unmarshal(v, &s.Title)
	}
	if v, ok := raw["paneTree"]; ok {
		s.PaneTree = append(json.RawMessage(nil), v...)
	}
	if v, ok := raw["openFiles"]; ok {
		s.OpenFiles = append(json.RawMessage(nil), v...)
	}
	if v, ok := raw["currentPath"]; ok {
		var cp string
		if err := json.Unmarshal(v, &cp); err == nil {
			s.CurrentPath = &cp
		}
	}
	if v, ok := raw["activePaneId"]; ok {
		var ap string
		if err := json.Unmarshal(v, &ap); err == nil {
			s.ActivePaneID = &ap
		}
	}
	if v, ok := raw["chainConfig"]; ok {
		s.ChainConfig = append(json.RawMessage(nil), v...)
	}
	for k, v := range raw {
		if known[k] {
			continue
		}
		s.Extra[k] = append(json.RawMessage(nil), v...)
	}
	return nil
}
