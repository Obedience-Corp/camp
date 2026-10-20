package workspace

import (
	"encoding/json"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// MergePushResult is the pure outcome of applying one agent tab to a document.
type MergePushResult struct {
	Document []byte
	TabID    string
	Action   string // appended | replaced
}

// MergePush applies an already-validated push spec onto existing ui-state bytes.
// Missing or empty documents become version 2 with only the pushed tab.
// User-owned tab IDs cannot be replaced. Unknown document/tab metadata is kept.
func MergePush(existing []byte, spec *PushSpec) (*MergePushResult, error) {
	if spec == nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "push spec is required")
	}
	doc, tabs, err := decodeDocument(existing)
	if err != nil {
		return nil, err
	}

	newTab, err := buildAgentTab(spec)
	if err != nil {
		return nil, err
	}
	tabID := spec.ID
	action := "appended"
	replaced := false
	outTabs := make([]json.RawMessage, 0, len(tabs)+1)
	for _, tabRaw := range tabs {
		id, origin, err := tabIdentity(tabRaw)
		if err != nil {
			return nil, err
		}
		if id != tabID {
			outTabs = append(outTabs, tabRaw)
			continue
		}
		if !isAgentOwned(origin) {
			return nil, camperrors.Wrapf(camperrors.ErrConflict,
				"refusing to replace user-owned workspace tab %q", tabID)
		}
		// Preserve unknown keys from the previous agent tab that the push omitted.
		mergedTab, err := preserveTabExtras(tabRaw, newTab)
		if err != nil {
			return nil, err
		}
		outTabs = append(outTabs, mergedTab)
		replaced = true
		action = "replaced"
	}
	if !replaced {
		outTabs = append(outTabs, newTab)
	}

	tabsJSON, err := json.Marshal(outTabs)
	if err != nil {
		return nil, err
	}
	doc["workspaceTabs"] = tabsJSON
	ver, _ := json.Marshal(DocumentVersion)
	doc["version"] = ver
	updated, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	doc["updatedAt"] = updated
	// Drop legacy root layout fields once we own a version-2 document.
	for _, k := range []string{"paneTree", "openFiles", "currentPath", "activePaneId", "chainConfig"} {
		delete(doc, k)
	}
	if _, ok := doc["activeWorkspaceTabId"]; !ok {
		active, _ := json.Marshal(tabID)
		doc["activeWorkspaceTabId"] = active
	}

	serialized, err := marshalDocument(doc)
	if err != nil {
		return nil, err
	}
	return &MergePushResult{Document: serialized, TabID: tabID, Action: action}, nil
}

func decodeDocument(existing []byte) (map[string]json.RawMessage, []json.RawMessage, error) {
	if len(bytesTrimSpace(existing)) == 0 {
		return map[string]json.RawMessage{}, nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil, nil, camperrors.Wrap(camperrors.ErrInvalidInput, "existing ui-state.json is not a JSON object")
	}
	tabsRaw, ok := doc["workspaceTabs"]
	// Empty/null workspaceTabs with root layout fields is still legacy: the
	// Festival app hydrates that shape as Tab 1. Migrate instead of treating
	// the document as an empty tab list and erasing root panes on push.
	if !ok || jsonNullOrEmptyArray(tabsRaw) {
		return migrateLegacyRootDocument(doc)
	}
	var tabs []json.RawMessage
	if err := json.Unmarshal(tabsRaw, &tabs); err != nil {
		return nil, nil, camperrors.Wrap(camperrors.ErrInvalidInput, "workspaceTabs must be an array")
	}
	return doc, tabs, nil
}

func jsonNullOrEmptyArray(raw json.RawMessage) bool {
	trimmed := bytesTrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return true
	}
	var tabs []json.RawMessage
	if err := json.Unmarshal(trimmed, &tabs); err != nil {
		return false
	}
	return len(tabs) == 0
}

// migrateLegacyRootDocument lifts root paneTree/openFiles/currentPath/
// activePaneId/chainConfig into DefaultUserTabID without stamping origin=agent.
// Detection matches festival-app parse_quest_ui_state / legacy_workspace_tab_state:
// null or empty openFiles alone is not legacy, so a modern empty layout stays empty.
func migrateLegacyRootDocument(doc map[string]json.RawMessage) (map[string]json.RawMessage, []json.RawMessage, error) {
	if !hasLegacyRootLayout(doc) {
		return doc, nil, nil
	}

	tab := map[string]json.RawMessage{}
	id, _ := json.Marshal(DefaultUserTabID)
	title, _ := json.Marshal(DefaultUserTabTitle)
	tab["id"] = id
	tab["title"] = title
	for _, key := range []string{"paneTree", "openFiles", "currentPath", "activePaneId", "chainConfig"} {
		if v, ok := doc[key]; ok && !jsonIsNull(v) {
			// Skip empty openFiles so the migrated tab matches app hydration.
			if key == "openFiles" && jsonNullOrEmptyArray(v) {
				continue
			}
			tab[key] = append(json.RawMessage(nil), v...)
		}
	}
	tabRaw, err := json.Marshal(tab)
	if err != nil {
		return nil, nil, err
	}
	return doc, []json.RawMessage{tabRaw}, nil
}

// hasLegacyRootLayout mirrors festival-app legacy_workspace_tab_state: a root
// layout exists when paneTree/currentPath/activePaneId/chainConfig is non-null
// or openFiles is a non-empty array. Mere key presence of empty/null fields does
// not invent a user tab.
func hasLegacyRootLayout(doc map[string]json.RawMessage) bool {
	if v, ok := doc["paneTree"]; ok && !jsonIsNull(v) {
		return true
	}
	if v, ok := doc["currentPath"]; ok && !jsonIsNull(v) {
		return true
	}
	if v, ok := doc["activePaneId"]; ok && !jsonIsNull(v) {
		return true
	}
	if v, ok := doc["chainConfig"]; ok && !jsonIsNull(v) {
		return true
	}
	if v, ok := doc["openFiles"]; ok && !jsonIsNull(v) && !jsonNullOrEmptyArray(v) {
		return true
	}
	return false
}

func jsonIsNull(raw json.RawMessage) bool {
	return string(bytesTrimSpace(raw)) == "null"
}

func buildAgentTab(spec *PushSpec) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	id, _ := json.Marshal(spec.ID)
	title, _ := json.Marshal(spec.Title)
	origin, _ := json.Marshal(OriginAgent)
	obj["id"] = id
	obj["title"] = title
	obj["origin"] = origin
	obj["paneTree"] = append(json.RawMessage(nil), spec.PaneTree...)
	if len(spec.OpenFiles) > 0 {
		obj["openFiles"] = append(json.RawMessage(nil), spec.OpenFiles...)
	}
	if spec.CurrentPath != nil {
		cp, _ := json.Marshal(*spec.CurrentPath)
		obj["currentPath"] = cp
	}
	if spec.ActivePaneID != nil {
		ap, _ := json.Marshal(*spec.ActivePaneID)
		obj["activePaneId"] = ap
	}
	if len(spec.ChainConfig) > 0 {
		obj["chainConfig"] = append(json.RawMessage(nil), spec.ChainConfig...)
	}
	for k, v := range spec.Extra {
		if k == "origin" || k == "id" || k == "title" || k == "paneTree" {
			continue
		}
		obj[k] = append(json.RawMessage(nil), v...)
	}
	return marshalOrderedObject(obj, []string{
		"id", "title", "origin", "paneTree", "openFiles", "currentPath", "activePaneId", "chainConfig",
	})
}

func preserveTabExtras(existing, incoming json.RawMessage) (json.RawMessage, error) {
	var oldObj, newObj map[string]json.RawMessage
	if err := json.Unmarshal(existing, &oldObj); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(incoming, &newObj); err != nil {
		return nil, err
	}
	for k, v := range oldObj {
		if _, ok := newObj[k]; !ok {
			newObj[k] = v
		}
	}
	// Incoming always wins for owned fields and origin.
	return marshalOrderedObject(newObj, []string{
		"id", "title", "origin", "paneTree", "openFiles", "currentPath", "activePaneId", "chainConfig",
	})
}

func tabIdentity(tabRaw json.RawMessage) (id, origin string, err error) {
	var tab map[string]json.RawMessage
	if err := json.Unmarshal(tabRaw, &tab); err != nil {
		return "", "", camperrors.Wrap(camperrors.ErrInvalidInput, "workspace tab must be an object")
	}
	if v, ok := tab["id"]; ok {
		_ = json.Unmarshal(v, &id)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", camperrors.Wrap(camperrors.ErrInvalidInput, "workspace tab id is required")
	}
	if v, ok := tab["origin"]; ok && string(v) != "null" {
		_ = json.Unmarshal(v, &origin)
	}
	return id, strings.TrimSpace(origin), nil
}

func isAgentOwned(origin string) bool {
	return strings.EqualFold(origin, OriginAgent)
}

func marshalDocument(doc map[string]json.RawMessage) ([]byte, error) {
	raw, err := marshalOrderedObject(doc, []string{
		"version", "updatedAt", "activeWorkspaceTabId", "workspaceTabs",
	})
	if err != nil {
		return nil, err
	}
	// Pretty-print to match festival-app serialize_quest_ui_state (to_string_pretty).
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		return nil, err
	}
	return json.MarshalIndent(pretty, "", "  ")
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
