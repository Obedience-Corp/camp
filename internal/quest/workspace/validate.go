package workspace

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var allowedPaneTypes = map[string]bool{
	"chat": true, "file": true, "workitem": true, "webview": true,
	"terminal": true, "empty": true,
}

// ValidateDocument checks a version-2 (or legacy-normalized) workspace document.
// campaignRoot may be empty to skip path boundary checks (structural only).
func ValidateDocument(campaignRoot string, raw []byte) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "workspace document is not JSON object")
	}
	tabsRaw, ok := doc["workspaceTabs"]
	if !ok {
		// Legacy root paneTree documents are accepted for show/validate of
		// existing files; push always writes version 2.
		if _, has := doc["paneTree"]; has {
			return nil
		}
		return camperrors.Wrap(camperrors.ErrInvalidInput, "workspace document missing workspaceTabs")
	}
	var tabs []json.RawMessage
	if err := json.Unmarshal(tabsRaw, &tabs); err != nil {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "workspaceTabs must be an array")
	}
	seen := map[string]bool{}
	for i, tabRaw := range tabs {
		id, err := validateTab(campaignRoot, tabRaw, fmt.Sprintf("workspaceTabs[%d]", i))
		if err != nil {
			return err
		}
		if seen[id] {
			return camperrors.Wrapf(camperrors.ErrInvalidInput, "duplicate workspace tab id %q", id)
		}
		seen[id] = true
	}
	return nil
}

// ValidatePushSpec validates and canonicalizes a push tab before any write.
func ValidatePushSpec(campaignRoot string, spec *PushSpec) error {
	if spec == nil {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "push spec is required")
	}
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "push spec id is required")
	}
	if strings.TrimSpace(spec.Title) == "" {
		spec.Title = "Agent"
	}
	if len(spec.PaneTree) == 0 || string(spec.PaneTree) == "null" {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "push spec paneTree is required")
	}
	canonical, err := canonicalizePaneTree(campaignRoot, spec.PaneTree)
	if err != nil {
		return err
	}
	spec.PaneTree = canonical
	if spec.CurrentPath != nil && strings.TrimSpace(*spec.CurrentPath) != "" {
		rel, err := CanonicalizeCampPath(campaignRoot, *spec.CurrentPath)
		if err != nil {
			return camperrors.Wrap(err, "currentPath")
		}
		spec.CurrentPath = &rel
	}
	return nil
}

func validateTab(campaignRoot string, tabRaw json.RawMessage, loc string) (string, error) {
	var tab map[string]json.RawMessage
	if err := json.Unmarshal(tabRaw, &tab); err != nil {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "%s must be an object", loc)
	}
	var id string
	if v, ok := tab["id"]; ok {
		_ = json.Unmarshal(v, &id)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "%s.id is required", loc)
	}
	if v, ok := tab["paneTree"]; ok && string(v) != "null" {
		if _, err := canonicalizePaneTree(campaignRoot, v); err != nil {
			return "", camperrors.Wrapf(err, "%s.paneTree", loc)
		}
	}
	return id, nil
}

func canonicalizePaneTree(campaignRoot string, raw json.RawMessage) (json.RawMessage, error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "paneTree must be an object")
	}
	var nodeType string
	if v, ok := node["type"]; ok {
		_ = json.Unmarshal(v, &nodeType)
	}
	var id string
	if v, ok := node["id"]; ok {
		_ = json.Unmarshal(v, &id)
	}
	if strings.TrimSpace(id) == "" {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "paneTree node id is required")
	}
	switch nodeType {
	case "pane":
		if contentRaw, ok := node["content"]; ok && string(contentRaw) != "null" {
			canonical, err := canonicalizePaneContent(campaignRoot, contentRaw)
			if err != nil {
				return nil, err
			}
			node["content"] = canonical
		}
		delete(node, "children")
		delete(node, "direction")
		delete(node, "sizes")
	case "split":
		var children []json.RawMessage
		if v, ok := node["children"]; ok {
			if err := json.Unmarshal(v, &children); err != nil {
				return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "split children must be an array")
			}
		}
		if len(children) != 2 {
			return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "split must have exactly two children")
		}
		out := make([]json.RawMessage, 2)
		for i, child := range children {
			c, err := canonicalizePaneTree(campaignRoot, child)
			if err != nil {
				return nil, camperrors.Wrapf(err, "children[%d]", i)
			}
			out[i] = c
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		node["children"] = encoded
		var direction string
		if v, ok := node["direction"]; ok {
			_ = json.Unmarshal(v, &direction)
		}
		if direction != "horizontal" && direction != "vertical" {
			return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "split direction must be horizontal or vertical")
		}
		delete(node, "content")
	default:
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "paneTree type %q is invalid", nodeType)
	}
	return marshalOrderedObject(node, []string{"id", "type", "content", "direction", "children", "sizes"})
}

func canonicalizePaneContent(campaignRoot string, raw json.RawMessage) (json.RawMessage, error) {
	var content map[string]json.RawMessage
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "pane content must be an object")
	}
	var paneType string
	if v, ok := content["type"]; ok {
		_ = json.Unmarshal(v, &paneType)
	}
	if !allowedPaneTypes[paneType] {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "pane content type %q is invalid", paneType)
	}

	// Runtime-only bindings are never durable.
	delete(content, "browserTabId")
	delete(content, "terminalSessionId")

	switch paneType {
	case "file":
		if v, ok := content["filePath"]; ok && string(v) != "null" {
			var fp string
			if err := json.Unmarshal(v, &fp); err != nil {
				return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "filePath must be a string")
			}
			if campaignRoot != "" && strings.TrimSpace(fp) != "" {
				rel, err := CanonicalizeCampPath(campaignRoot, fp)
				if err != nil {
					return nil, camperrors.Wrap(err, "filePath")
				}
				encoded, _ := json.Marshal(rel)
				content["filePath"] = encoded
			}
		}
	case "workitem":
		if v, ok := content["workitemPath"]; ok && string(v) != "null" {
			var wp string
			if err := json.Unmarshal(v, &wp); err != nil {
				return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "workitemPath must be a string")
			}
			if campaignRoot != "" && strings.TrimSpace(wp) != "" {
				rel, err := CanonicalizeCampPath(campaignRoot, wp)
				if err != nil {
					return nil, camperrors.Wrap(err, "workitemPath")
				}
				encoded, _ := json.Marshal(rel)
				content["workitemPath"] = encoded
			}
		}
		if v, ok := content["workitemAnchor"]; ok && string(v) != "null" {
			var anchor map[string]json.RawMessage
			if err := json.Unmarshal(v, &anchor); err != nil {
				return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "workitemAnchor must be an object")
			}
			if p, ok := anchor["path"]; ok {
				var ap string
				if err := json.Unmarshal(p, &ap); err == nil && campaignRoot != "" && strings.TrimSpace(ap) != "" {
					rel, err := CanonicalizeCampPath(campaignRoot, ap)
					if err != nil {
						return nil, camperrors.Wrap(err, "workitemAnchor.path")
					}
					encoded, _ := json.Marshal(rel)
					anchor["path"] = encoded
					encodedAnchor, err := marshalOrderedObject(anchor, []string{"path", "offset"})
					if err != nil {
						return nil, err
					}
					content["workitemAnchor"] = encodedAnchor
				}
			}
		}
	case "webview":
		var url string
		if v, ok := content["url"]; ok && string(v) != "null" {
			_ = json.Unmarshal(v, &url)
		}
		if strings.TrimSpace(url) == "" {
			return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "webview pane requires durable url")
		}
	}

	return marshalOrderedObject(content, []string{
		"type", "url", "filePath", "workitemPath", "workitemAnchor",
		"chatSessionId", "chatDraft", "chatDraftsBySession",
	})
}

func marshalOrderedObject(obj map[string]json.RawMessage, preferred []string) (json.RawMessage, error) {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	write := func(k string, v json.RawMessage) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(v)
	}
	seen := map[string]bool{}
	for _, k := range preferred {
		if v, ok := obj[k]; ok {
			write(k, v)
			seen[k] = true
		}
	}
	// Preserve unknown keys in sorted order for stable generation bytes.
	extras := make([]string, 0)
	for k := range obj {
		if !seen[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, k := range extras {
		write(k, obj[k])
	}
	b.WriteByte('}')
	return json.RawMessage(b.String()), nil
}
