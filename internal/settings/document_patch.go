package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// PatchChange is one object-key edit. Set requires Value, including JSON null.
// Remove rejects a value. Path addresses nested object keys; replace an array
// by setting it as the value.
type PatchChange struct {
	Op       string
	Path     []string
	Value    json.RawMessage
	ValueSet bool
}

type patchDocument struct {
	Changes []json.RawMessage `json:"changes"`
}

type patchChangeRaw struct {
	Op    string          `json:"op"`
	Path  []string        `json:"path"`
	Value json.RawMessage `json:"value"`
	set   bool
}

func (c *patchChangeRaw) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["op"]; ok {
		if err := json.Unmarshal(raw, &c.Op); err != nil {
			return err
		}
	}
	if raw, ok := fields["path"]; ok {
		if err := json.Unmarshal(raw, &c.Path); err != nil {
			return err
		}
	}
	if raw, ok := fields["value"]; ok {
		c.Value = raw
		c.set = true
	}
	return nil
}

// ParsePatch decodes a patch body. A set without value, or a remove with a
// value, is rejected.
func ParsePatch(source []byte) ([]PatchChange, error) {
	if len(source) > DefaultLimits().MaxDocumentBytes {
		return nil, camperrors.NewValidation("patch", "document exceeds the size limit", nil)
	}
	var doc patchDocument
	if err := json.Unmarshal(source, &doc); err != nil {
		return nil, camperrors.NewValidation("patch", "patch must be a JSON object with changes", err)
	}
	if doc.Changes == nil {
		return nil, camperrors.NewValidation("patch", "changes is required", nil)
	}
	out := make([]PatchChange, 0, len(doc.Changes))
	for _, raw := range doc.Changes {
		var change patchChangeRaw
		if err := json.Unmarshal(raw, &change); err != nil {
			return nil, camperrors.NewValidation("patch", "invalid patch change", err)
		}
		if err := checkPatchChange(change); err != nil {
			return nil, err
		}
		out = append(out, PatchChange{
			Op:       change.Op,
			Path:     change.Path,
			Value:    change.Value,
			ValueSet: change.set,
		})
	}
	return out, nil
}

func checkPatchChange(change patchChangeRaw) error {
	switch change.Op {
	case "set":
		if !change.set {
			return camperrors.NewValidation("patch", "set requires value", nil)
		}
	case "remove":
		if change.set {
			return camperrors.NewValidation("patch", "remove does not take a value", nil)
		}
	default:
		return camperrors.NewValidation("patch", "op must be set or remove", nil)
	}
	if len(change.Path) == 0 {
		return camperrors.NewValidation("patch", "path is required", nil)
	}
	return nil
}

// PatchYAML applies changes to a YAML mapping, preserving comments on nodes
// that the patch does not replace and preserving unknown fields.
func PatchYAML(source []byte, changes []PatchChange) ([]byte, error) {
	doc, err := parseYAMLDocument(source)
	if err != nil {
		return nil, err
	}
	mapping, err := mappingRoot(doc)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		if err := applyYAMLChange(mapping, change); err != nil {
			return nil, err
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal patched yaml")
	}
	return out, nil
}

// PatchJSON applies changes to a JSON object. Unknown keys stay. Null stores
// an explicit null; remove deletes the key. Array values replace the key.
func PatchJSON(source []byte, changes []PatchChange) ([]byte, error) {
	root, err := parseJSONObject(source)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		if err := applyJSONChange(root, change); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal patched json")
	}
	out = append(out, '\n')
	return out, nil
}

func parseYAMLDocument(source []byte) (*yaml.Node, error) {
	doc, err := inspectYAML(source)
	if err != nil {
		return nil, err
	}
	if yamlHasAliasOrMerge(doc) {
		return nil, camperrors.NewValidation("patch", "YAML aliases and merge keys are not patched; edit the source", nil)
	}
	return doc, nil
}

func keepNodeComments(old, next *yaml.Node) {
	if old == nil || next == nil {
		return
	}
	if next.HeadComment == "" {
		next.HeadComment = old.HeadComment
	}
	if next.LineComment == "" {
		next.LineComment = old.LineComment
	}
	if next.FootComment == "" {
		next.FootComment = old.FootComment
	}
}

func mappingRoot(doc *yaml.Node) (*yaml.Node, error) {
	node := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			doc.Content = []*yaml.Node{mapping}
			return mapping, nil
		}
		node = doc.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil, camperrors.NewValidation("patch", "document root must be a mapping", nil)
	}
	return node, nil
}

func applyYAMLChange(mapping *yaml.Node, change PatchChange) error {
	if len(change.Path) == 0 {
		return camperrors.NewValidation("patch", "path is required", nil)
	}
	if len(change.Path) == 1 {
		return editYAMLKey(mapping, change.Path[0], change)
	}
	child, err := yamlChildMapping(mapping, change.Path[0], change.Op == "set")
	if err != nil || child == nil {
		return err
	}
	return applyYAMLChange(child, PatchChange{
		Op: change.Op, Path: change.Path[1:], Value: change.Value, ValueSet: change.ValueSet,
	})
}

func editYAMLKey(mapping *yaml.Node, key string, change PatchChange) error {
	idx := yamlKeyIndex(mapping, key)
	if change.Op == "remove" {
		if idx < 0 {
			return nil
		}
		mapping.Content = append(mapping.Content[:idx], mapping.Content[idx+2:]...)
		return nil
	}
	value, err := yamlValue(change.Value)
	if err != nil {
		return err
	}
	if idx >= 0 {
		keepNodeComments(mapping.Content[idx+1], value)
		mapping.Content[idx+1] = value
		return nil
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
	return nil
}

func yamlChildMapping(mapping *yaml.Node, key string, create bool) (*yaml.Node, error) {
	idx := yamlKeyIndex(mapping, key)
	if idx < 0 {
		if !create {
			return nil, nil
		}
		child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			child,
		)
		return child, nil
	}
	child := mapping.Content[idx+1]
	if child.Kind != yaml.MappingNode {
		return nil, camperrors.NewValidation("patch", "path enters a value that is not an object", nil)
	}
	return child, nil
}

func yamlKeyIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return i
		}
	}
	return -1
}

func yamlValue(raw json.RawMessage) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	node, err := jsonTokenToYAML(dec)
	if err != nil {
		return nil, camperrors.NewValidation("patch", "value must be JSON", err)
	}
	if dec.More() {
		return nil, camperrors.NewValidation("patch", "value must be a single JSON value", nil)
	}
	return node, nil
}

func jsonTokenToYAML(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, camperrors.NewValidation("patch", "object key must be a string", nil)
				}
				value, err := jsonTokenToYAML(dec)
				if err != nil {
					return nil, err
				}
				mapping.Content = append(mapping.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
					value,
				)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return mapping, nil
		case '[':
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				value, err := jsonTokenToYAML(dec)
				if err != nil {
					return nil, err
				}
				seq.Content = append(seq.Content, value)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return seq, nil
		default:
			return nil, camperrors.NewValidation("patch", "unexpected JSON delimiter", nil)
		}
	}
	switch value := tok.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case bool:
		text := "false"
		if value {
			text = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: text}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(value.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value.String()}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	default:
		return nil, camperrors.NewValidation("patch", "unsupported JSON value", nil)
	}
}

func parseJSONObject(source []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(source)) == 0 {
		return map[string]any{}, nil
	}
	decoded, err := decodeJSONValue(source)
	if err != nil {
		return nil, camperrors.Wrap(err, "parse json")
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, camperrors.NewValidation("patch", "document root must be an object", nil)
	}
	return object, nil
}

func inspectYAML(source []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(source)) == 0 || isCommentOnly(source) {
		return emptyYAMLDocument(commentHeader(source)), nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(source))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, camperrors.Wrap(err, "parse yaml")
	}
	var extra yaml.Node
	extraErr := dec.Decode(&extra)
	if extraErr == nil {
		return nil, camperrors.NewValidation("yaml", "multiple YAML documents are not accepted", nil)
	}
	if !errors.Is(extraErr, io.EOF) {
		return nil, camperrors.Wrap(extraErr, "parse yaml")
	}
	if doc.Kind == 0 || (doc.Kind == yaml.DocumentNode && len(doc.Content) == 0) {
		return emptyYAMLDocument(commentHeader(source)), nil
	}
	if err := duplicateYAMLKeys(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func isCommentOnly(source []byte) bool {
	saw := false
	for line := range strings.SplitSeq(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return false
		}
		saw = true
	}
	return saw
}

func emptyYAMLDocument(head string) *yaml.Node {
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: head}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}
}

func commentHeader(source []byte) string {
	var lines []string
	for line := range strings.SplitSeq(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return ""
		}
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
	}
	return strings.Join(lines, "\n")
}

func duplicateYAMLKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind == yaml.ScalarNode {
				if seen[key.Value] {
					return camperrors.NewValidation("yaml", "duplicate key "+key.Value, nil)
				}
				seen[key.Value] = true
			}
			if err := duplicateYAMLKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := duplicateYAMLKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func yamlHasAliasOrMerge(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.AliasNode {
		return true
	}
	if node.Kind == yaml.ScalarNode && node.Value == "<<" {
		return true
	}
	for _, child := range node.Content {
		if yamlHasAliasOrMerge(child) {
			return true
		}
	}
	return false
}

func applyJSONChange(object map[string]any, change PatchChange) error {
	if len(change.Path) == 0 {
		return camperrors.NewValidation("patch", "path is required", nil)
	}
	parent := object
	for _, key := range change.Path[:len(change.Path)-1] {
		next, ok := parent[key]
		if !ok || next == nil {
			if change.Op != "set" {
				return nil
			}
			child := map[string]any{}
			parent[key] = child
			parent = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return camperrors.NewValidation("patch", "path enters a value that is not an object", nil)
		}
		parent = child
	}
	key := change.Path[len(change.Path)-1]
	if change.Op == "remove" {
		delete(parent, key)
		return nil
	}
	value, err := decodeJSONValue(change.Value)
	if err != nil {
		return camperrors.NewValidation("patch", "value must be JSON", err)
	}
	parent[key] = value
	return nil
}
