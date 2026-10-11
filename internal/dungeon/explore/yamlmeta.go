package explore

import (
	"context"
	"strings"

	"gopkg.in/yaml.v3"
)

// festMeta is what the feed takes from fest.yaml.
type festMeta struct {
	id      string
	name    string
	goal    string
	history []historyEntry
}

// workitemMeta is what the feed takes from a .workitem marker.
type workitemMeta struct {
	id    string
	title string
	ref   string
}

// readFestMeta reads a fest.yaml. A file past the read limit gives the fields
// found inside it and errMetaCut: the cut is what is wrong with the row,
// whatever the parser then made of where the text stops.
func readFestMeta(ctx context.Context, path string) (festMeta, error) {
	text, cut, err := readLimited(ctx, path)
	if err != nil {
		return festMeta{}, err
	}
	meta, err := parseFestMeta(text)
	if cut {
		err = errMetaCut
	}
	return meta, err
}

// readWorkitemMeta reads a .workitem marker the way readFestMeta reads a
// fest.yaml.
func readWorkitemMeta(ctx context.Context, path string) (workitemMeta, error) {
	text, cut, err := readLimited(ctx, path)
	if err != nil {
		return workitemMeta{}, err
	}
	meta, err := parseWorkitemMeta(text)
	if cut {
		err = errMetaCut
	}
	return meta, err
}

// parseFestMeta reads the metadata block of a fest.yaml. It returns every
// field it could read together with the first thing wrong with the file, so
// one bad field costs that field and not the row.
func parseFestMeta(text string) (festMeta, error) {
	var meta festMeta
	root, err := parseYAML(text)
	if err != nil || root == nil {
		return meta, err
	}
	walk := yamlWalk{budget: metaReadLimit}
	top := walk.fields("", root)
	section := top.node("metadata")
	if section == nil {
		return meta, walk.problem
	}
	fields := walk.fields("metadata", section)
	meta.id = walk.text(fields, "id")
	meta.name = walk.text(fields, "name")
	meta.goal = walk.text(fields, "goal")
	for _, entry := range walk.list(fields, "status_history") {
		if walk.budget < 0 {
			break
		}
		step := walk.fields("status_history", entry)
		meta.history = append(meta.history, historyEntry{
			Status:    walk.text(step, "status"),
			Timestamp: walk.text(step, "timestamp"),
		})
	}
	return meta, walk.problem
}

// parseWorkitemMeta reads a .workitem marker the way parseFestMeta reads a
// fest.yaml.
func parseWorkitemMeta(text string) (workitemMeta, error) {
	var meta workitemMeta
	root, err := parseYAML(text)
	if err != nil || root == nil {
		return meta, err
	}
	walk := yamlWalk{budget: metaReadLimit}
	fields := walk.fields("", root)
	meta.id = walk.text(fields, "id")
	meta.title = walk.text(fields, "title")
	meta.ref = walk.text(fields, "ref")
	return meta, walk.problem
}

// unmarshalYAML is the parser. Tests replace it to make it panic.
var unmarshalYAML = yaml.Unmarshal

// parseYAML parses the first document in text into a node tree, nil for an
// empty one. The tree is built in one pass and nothing is decoded into Go
// values: that decoder compares every pair of keys in a mapping, and it
// panics on a merge key beside a key that is itself a mapping. A panic in
// the parser is returned as an error.
func parseYAML(text string) (root *yaml.Node, err error) {
	defer func() {
		if recover() != nil {
			root, err = nil, errYAMLParse
		}
	}()
	var doc yaml.Node
	if err := unmarshalYAML([]byte(text), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	return doc.Content[0], nil
}

// yamlWalk reads named fields out of a node tree. It keeps the first thing
// wrong with the document. budget is how many nodes it may still visit: an
// alias lets a small file name one large mapping many times over, so the
// walk stops once it has visited more nodes than the largest file the feed
// reads could hold.
type yamlWalk struct {
	budget  int
	problem error
}

// yamlFields is the entries of one mapping whose keys are plain names.
type yamlFields map[string]*yaml.Node

// node is the value under key with an alias followed once, nil when the key
// is missing or its value is null.
func (f yamlFields) node(key string) *yaml.Node {
	return present(f[key])
}

func (w *yamlWalk) note(err error) {
	if w.problem == nil {
		w.problem = err
	}
}

// fields visits explicit keys before merged mappings. Earlier mappings in a
// merge sequence win, and explicit keys (including nulls) win over all merges.
// The explicit stack avoids recursive alias expansion and copying accumulated
// maps at each merge. Every visited node consumes the shared traversal budget.
func (w *yamlWalk) fields(name string, node *yaml.Node) yamlFields {
	node = present(node)
	if node == nil {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		w.note(errNotMapping(name))
		return nil
	}
	type visit struct {
		node    *yaml.Node
		leaving bool
	}
	stack := []visit{{node: node}}
	active := map[*yaml.Node]bool{}
	fields := yamlFields{}
	for len(stack) > 0 {
		task := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node = task.node
		if task.leaving {
			delete(active, node)
			continue
		}
		if active[node] {
			w.note(errYAMLMergeCycle)
			continue
		}
		// Charge the mapping itself as well as its keys and values, so
		// aliases of empty mappings cannot bypass the work limit.
		if !w.spend(1 + len(node.Content)) {
			break
		}
		active[node] = true
		stack = append(stack, visit{node: node, leaving: true})
		seen := map[string]bool{}
		var merge *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				w.note(errYAMLKey)
				continue
			}
			if seen[key.Value] {
				w.note(errRepeatedKey(key.Value))
				continue
			}
			seen[key.Value] = true
			if key.Value == "<<" && key.ShortTag() == "!!merge" {
				merge = node.Content[i+1]
				continue
			}
			if _, exists := fields[key.Value]; !exists {
				fields[key.Value] = node.Content[i+1]
			}
		}
		sources := w.mergeSources(merge)
		// Stack order implements the YAML rule that the first source wins.
		for i := len(sources) - 1; i >= 0; i-- {
			stack = append(stack, visit{node: sources[i]})
		}
	}
	return fields
}

func (w *yamlWalk) spend(nodes int) bool {
	w.budget -= nodes
	if w.budget < 0 {
		w.note(errYAMLTooMany)
		return false
	}
	return true
}

// mergeSources validates the merge shape without decoding arbitrary YAML
// values into Go maps (which would restore the decoder's quadratic key scan).
func (w *yamlWalk) mergeSources(raw *yaml.Node) []*yaml.Node {
	if raw == nil {
		return nil
	}
	node := present(raw)
	if node != nil && node.Kind == yaml.MappingNode {
		return []*yaml.Node{node}
	}
	if node == nil || node.Kind != yaml.SequenceNode {
		w.note(errYAMLMerge)
		return nil
	}
	if !w.spend(1 + len(node.Content)) {
		return nil
	}
	sources := make([]*yaml.Node, 0, len(node.Content))
	for _, source := range node.Content {
		source = present(source)
		if source == nil || source.Kind != yaml.MappingNode {
			w.note(errYAMLMerge)
			continue
		}
		sources = append(sources, source)
	}
	return sources
}

// text is the value under key as the author wrote it, empty when the key is
// missing or null. A list or a mapping where one value belongs is noted.
func (w *yamlWalk) text(fields yamlFields, key string) string {
	node := fields.node(key)
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		w.note(errNotValue(key))
		return ""
	}
	return strings.TrimSpace(node.Value)
}

// list is the entries of the sequence under key.
func (w *yamlWalk) list(fields yamlFields, key string) []*yaml.Node {
	node := fields.node(key)
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		w.note(errNotList(key))
		return nil
	}
	return node.Content
}

// present follows an alias once and reports a null as nothing. One step is
// enough: an anchor cannot sit on an alias, and following further could loop
// on an anchor that contains itself.
func present(node *yaml.Node) *yaml.Node {
	if node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node == nil || node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null" {
		return nil
	}
	return node
}
